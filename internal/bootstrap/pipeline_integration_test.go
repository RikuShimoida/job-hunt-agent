//go:build integration

// リポジトリ直下の testdata と実 SQLite を使い、collect → score → notify(dry-run) を
// 一気通貫で検証する。外部サービスへは接続しない。
//
// 再現手順:
//
//	go test -tags=integration ./internal/bootstrap/...
package bootstrap_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
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

// setup はリポジトリ直下へ移動し、一時 DB を使うアプリを組み立てる。
//
// sources.example.yaml の path が testdata/... のリポジトリ相対のため、
// 実行ディレクトリをリポジトリ直下へ移す必要がある。
func setup(t *testing.T, status string, out, logOut *bytes.Buffer) *bootstrap.App {
	t.Helper()

	profilePath := writeProfile(t, repoRoot, status)
	sourcesPath := filepath.Join(repoRoot, "config", "sources.example.yaml")

	t.Chdir(repoRoot)
	t.Setenv("DATABASE_URL", filepath.Join(t.TempDir(), "it.db"))
	t.Setenv("LOG_LEVEL", "info")

	app, err := bootstrap.New(context.Background(), bootstrap.Options{
		ProfilePath: profilePath,
		SourcesPath: sourcesPath,
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

	// 常駐必須の案件は除外される。
	if summary.Score.RejectedCount < 1 {
		t.Errorf("RejectedCount = %d, want >= 1（常駐案件が除外されるはず）",
			summary.Score.RejectedCount)
	}
	if summary.Notify.NotifiedCount < 1 {
		t.Fatalf("NotifiedCount = %d, want >= 1", summary.Notify.NotifiedCount)
	}

	printed := out.String()

	// 通知に必要な項目が dry-run 出力へ現れていること。
	for _, want := range []string{"点・新着", "単価：", "稼働：", "勤務：", "加点：", "URL："} {
		if !strings.Contains(printed, want) {
			t.Errorf("dry-run 出力に %q が含まれていない\n--- 出力 ---\n%s", want, printed)
		}
	}

	// 除外された常駐案件が通知に混ざっていないこと。
	if strings.Contains(printed, "PHP 保守運用案件") {
		t.Errorf("除外されるはずの常駐案件が通知された\n--- 出力 ---\n%s", printed)
	}

	// 実行サマリがログに残っていること。
	logs := logOut.String()
	for _, want := range []string{"実行サマリ", "fetched", "new", "duplicate", "notified", "failed_sources"} {
		if !strings.Contains(logs, want) {
			t.Errorf("ログに %q が含まれていない\n--- ログ ---\n%s", want, logs)
		}
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
	if second.Notify.NotifiedCount != first.Notify.NotifiedCount {
		t.Errorf("2回目の NotifiedCount = %d, want %d（案件が増えていない）",
			second.Notify.NotifiedCount, first.Notify.NotifiedCount)
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
	if watchingSummary.Notify.NotifiedCount > searchingSummary.Notify.NotifiedCount {
		t.Errorf("watching の通知 %d件が searching の %d件より多い（閾値が効いていない）",
			watchingSummary.Notify.NotifiedCount, searchingSummary.Notify.NotifiedCount)
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
	if summary.Notify.NotifiedCount != 0 {
		t.Errorf("paused なのに %d件通知された", summary.Notify.NotifiedCount)
	}
	if out.Len() != 0 {
		t.Errorf("paused なのに通知が出力された: %q", out.String())
	}
}
