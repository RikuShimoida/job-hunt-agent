//go:build integration

// リポジトリ直下の testdata と実 SQLite を使い、collect → score → notify を
// 一気通貫で検証する。Slack へは httptest.Server を宛先にして接続する
// （実 Slack ワークスペースへは送らない）。
//
// 再現手順:
//
//	go test -tags=integration ./internal/bootstrap/...
package bootstrap_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/RikuShimoida/job-hunt-agent/internal/bootstrap"
)

// repoRoot はリポジトリ直下の絶対パス。
//
// setup() が t.Chdir で作業ディレクトリを移すため、"../.." を都度解決すると
// 2回目以降の呼び出しで基準がずれる。テストバイナリ起動時（パッケージ
// ディレクトリにいる時点）に1度だけ確定させる。
var repoRoot = func() string {
	root, err := filepath.Abs("../..")
	if err != nil {
		panic("failed to resolve repo root: " + err.Error())
	}
	return root
}()

// setup はリポジトリ直下へ移動し、一時 DB を使う dry-run アプリを組み立てる。
//
// sources.example.yaml の path が testdata/... のリポジトリ相対のため、
// 実行ディレクトリをリポジトリ直下へ移す必要がある。
func setup(t *testing.T, status string, out, logOut *bytes.Buffer) *bootstrap.App {
	t.Helper()
	return newApp(t, status, true, out, logOut)
}

func newApp(t *testing.T, status string, dryRun bool, out, logOut *bytes.Buffer) *bootstrap.App {
	t.Helper()

	profilePath := writeProfile(t, repoRoot, status)
	sourcesPath := filepath.Join(repoRoot, "config", "sources.example.yaml")

	t.Chdir(repoRoot)
	t.Setenv("DATABASE_URL", filepath.Join(t.TempDir(), "it.db"))
	t.Setenv("LOG_LEVEL", "info")

	app, err := bootstrap.New(context.Background(), bootstrap.Options{
		ProfilePath: profilePath,
		SourcesPath: sourcesPath,
		DryRun:      dryRun,
		Out:         out,
		LogOut:      logOut,
	})
	if err != nil {
		t.Fatalf("bootstrap.New() returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Errorf("failed to close app: %v", err)
		}
	})
	return app
}

// writeProfile は example プロフィールを基に、search_status だけ差し替えた設定を書く。
func writeProfile(t *testing.T, root, status string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(root, "config", "profile.example.yaml"))
	if err != nil {
		t.Fatalf("failed to read example profile: %v", err)
	}

	replaced := strings.Replace(string(body),
		"search_status: searching", "search_status: "+status, 1)

	path := filepath.Join(t.TempDir(), "profile.yaml")
	if err := os.WriteFile(path, []byte(replaced), 0o600); err != nil {
		t.Fatalf("failed to write profile: %v", err)
	}
	return path
}

// slackServer は Slack Webhook の代わりに受信内容を記録する。
type slackServer struct {
	mu sync.Mutex

	texts []string
	srv   *httptest.Server
}

func newSlackServer(t *testing.T) *slackServer {
	t.Helper()

	s := &slackServer{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var p struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		s.mu.Lock()
		s.texts = append(s.texts, p.Text)
		s.mu.Unlock()

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *slackServer) received() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]string, len(s.texts))
	copy(out, s.texts)
	return out
}

func TestPipelineEndToEnd(t *testing.T) {
	ctx := context.Background()
	var out, logOut bytes.Buffer

	app := setup(t, "searching", &out, &logOut)

	summary, err := app.Pipeline.Run(ctx, app.Profile)
	if err != nil {
		t.Fatalf("Pipeline.Run() returned error: %v", err)
	}

	// testdata: メール4件 + HTML2件 = 6件。
	if summary.Collect.FetchedCount != 6 {
		t.Errorf("FetchedCount = %d, want 6", summary.Collect.FetchedCount)
	}
	if len(summary.Collect.FailedSources) != 0 {
		t.Errorf("FailedSources = %v, want []", summary.Collect.FailedSources)
	}

	// メール001と HTML001 は同じ URL を指すため、重複として1件にまとまる。
	// 6件のうち新規は5件。
	if summary.Collect.NewCount != 5 {
		t.Errorf("NewCount = %d, want 5", summary.Collect.NewCount)
	}
	if summary.Collect.DuplicateCount != 1 {
		t.Errorf("DuplicateCount = %d, want 1", summary.Collect.DuplicateCount)
	}

	// profile.example.yaml は remote_required: true（出社0日のみ許容）。
	// 保存される5件のうち、出社を伴う次の3件が除外される。
	//   - PHP 保守運用案件（常駐必須）
	//   - TypeScript／React フロント刷新案件（リモート可・週1出社）
	//   - Ruby on Rails 新規開発案件（週2日出社）
	// 残るフルリモート2件（Java／AWS・Go／Kubernetes）が通知される。
	if summary.Score.RejectedCount != 3 {
		t.Errorf("RejectedCount = %d, want 3（出社を伴う案件が除外されるはず）",
			summary.Score.RejectedCount)
	}
	if summary.Notify.TargetCount != 2 {
		t.Fatalf("TargetCount = %d, want 2", summary.Notify.TargetCount)
	}

	printed := out.String()

	// 通知に必要な項目が dry-run 出力へ現れていること。
	for _, want := range []string{"点・新着", "単価：", "稼働：", "勤務：", "加点：", "URL："} {
		if !strings.Contains(printed, want) {
			t.Errorf("dry-run 出力に %q が含まれていない\n--- 出力 ---\n%s", want, printed)
		}
	}

	// 出社を伴う案件が通知に混ざっていないこと。
	for _, unwanted := range []string{
		"PHP 保守運用案件",
		"TypeScript／React フロント刷新案件",
		"Ruby on Rails 新規開発案件",
	} {
		if strings.Contains(printed, unwanted) {
			t.Errorf("除外されるはずの案件が通知された: %q\n--- 出力 ---\n%s", unwanted, printed)
		}
	}

	// 実行サマリがログに残っていること。
	logs := logOut.String()
	for _, want := range []string{"実行サマリ", "fetched", "new", "duplicate", "notified", "failed_sources"} {
		if !strings.Contains(logs, want) {
			t.Errorf("ログに %q が含まれていない\n--- ログ ---\n%s", want, logs)
		}
	}
}

// TestPipelineSendsToSlackOnceAcrossRuns は、受入条件の中核を一気通貫で確かめる。
//
//   - 1回目の run で閾値以上の案件が Slack へ POST される
//   - 2回目の run では1件も送られない（通知済み管理）
func TestPipelineSendsToSlackOnceAcrossRuns(t *testing.T) {
	ctx := context.Background()
	var out, logOut bytes.Buffer

	slack := newSlackServer(t)
	t.Setenv("SLACK_WEBHOOK_URL", slack.srv.URL)

	app := newApp(t, "searching", false, &out, &logOut)

	first, err := app.Pipeline.Run(ctx, app.Profile)
	if err != nil {
		t.Fatalf("1回目の Run() でエラー: %v", err)
	}
	if first.Notify.SentCount != 2 {
		t.Fatalf("1回目の SentCount = %d, want 2", first.Notify.SentCount)
	}

	sent := slack.received()
	if len(sent) != 2 {
		t.Fatalf("Slack への POST = %d件, want 2件", len(sent))
	}

	// 送信本文に受入条件の項目が含まれること。
	joined := strings.Join(sent, "\n")
	for _, want := range []string{"点・新着", "単価：", "稼働：", "勤務：", "主要スキル：", "加点：", "URL："} {
		if !strings.Contains(joined, want) {
			t.Errorf("Slack 送信本文に %q が含まれていない\n--- 本文 ---\n%s", want, joined)
		}
	}

	second, err := app.Pipeline.Run(ctx, app.Profile)
	if err != nil {
		t.Fatalf("2回目の Run() でエラー: %v", err)
	}

	if second.Notify.TargetCount != 0 {
		t.Errorf("2回目の TargetCount = %d, want 0（同じ案件が再通知されている）",
			second.Notify.TargetCount)
	}
	if got := len(slack.received()); got != 2 {
		t.Errorf("2回目の実行後の POST 総数 = %d, want 2（2回目は送られないはず）", got)
	}
}

// TestPipelineDryRunDoesNotPostToSlack は、--dry-run で HTTP リクエストが
// 1件も飛ばないことを確かめる。
func TestPipelineDryRunDoesNotPostToSlack(t *testing.T) {
	ctx := context.Background()
	var out, logOut bytes.Buffer

	slack := newSlackServer(t)
	t.Setenv("SLACK_WEBHOOK_URL", slack.srv.URL)

	app := newApp(t, "searching", true, &out, &logOut)

	if _, err := app.Pipeline.Run(ctx, app.Profile); err != nil {
		t.Fatalf("Pipeline.Run() returned error: %v", err)
	}

	if got := len(slack.received()); got != 0 {
		t.Errorf("dry-run なのに Slack へ %d件 POST された, want 0件", got)
	}
	if !strings.Contains(out.String(), "点・新着") {
		t.Errorf("dry-run で送信予定の内容が標準出力に出ていない:\n%s", out.String())
	}
}

func TestPipelineRerunDoesNotDuplicate(t *testing.T) {
	ctx := context.Background()
	var out, logOut bytes.Buffer

	app := setup(t, "searching", &out, &logOut)

	first, err := app.Pipeline.Run(ctx, app.Profile)
	if err != nil {
		t.Fatalf("1回目の Run() でエラー: %v", err)
	}
	if first.Collect.NewCount == 0 {
		t.Fatal("1回目で1件も新規登録されていない")
	}

	second, err := app.Pipeline.Run(ctx, app.Profile)
	if err != nil {
		t.Fatalf("2回目の Run() でエラー: %v", err)
	}

	// 受入条件: 同じ案件を再実行しても無制限に重複登録されない。
	if second.Collect.NewCount != 0 {
		t.Errorf("2回目の NewCount = %d, want 0（重複登録されている）", second.Collect.NewCount)
	}
	if second.Collect.FetchedCount != first.Collect.FetchedCount {
		t.Errorf("2回目の FetchedCount = %d, want %d",
			second.Collect.FetchedCount, first.Collect.FetchedCount)
	}

	// dry-run は通知済みとして記録しないため、送信予定件数は変わらない。
	if second.Notify.TargetCount != first.Notify.TargetCount {
		t.Errorf("2回目の TargetCount = %d, want %d（案件が増えていない）",
			second.Notify.TargetCount, first.Notify.TargetCount)
	}
}

func TestPipelineWatchingRaisesThreshold(t *testing.T) {
	ctx := context.Background()

	var searchingOut, searchingLog bytes.Buffer
	searching := setup(t, "searching", &searchingOut, &searchingLog)
	searchingSummary, err := searching.Pipeline.Run(ctx, searching.Profile)
	if err != nil {
		t.Fatalf("searching の Run() でエラー: %v", err)
	}

	var watchingOut, watchingLog bytes.Buffer
	watching := setup(t, "watching", &watchingOut, &watchingLog)
	watchingSummary, err := watching.Pipeline.Run(ctx, watching.Profile)
	if err != nil {
		t.Fatalf("watching の Run() でエラー: %v", err)
	}

	// watching は閾値80のため、searching（閾値60）より通知が減るか同数になる。
	if watchingSummary.Notify.TargetCount > searchingSummary.Notify.TargetCount {
		t.Errorf("watching の通知 %d件が searching の %d件より多い（閾値が効いていない）",
			watchingSummary.Notify.TargetCount, searchingSummary.Notify.TargetCount)
	}
}

func TestPipelinePausedCollectsAndNotifiesNothing(t *testing.T) {
	ctx := context.Background()
	var out, logOut bytes.Buffer

	app := setup(t, "paused", &out, &logOut)

	summary, err := app.Pipeline.Run(ctx, app.Profile)
	if err != nil {
		t.Fatalf("Pipeline.Run() returned error: %v", err)
	}

	if summary.Collect.FetchedCount != 0 {
		t.Errorf("paused なのに %d件取得された", summary.Collect.FetchedCount)
	}
	if summary.Notify.TargetCount != 0 {
		t.Errorf("paused なのに %d件通知された", summary.Notify.TargetCount)
	}
	if out.Len() != 0 {
		t.Errorf("paused なのに通知が出力された: %q", out.String())
	}
}
