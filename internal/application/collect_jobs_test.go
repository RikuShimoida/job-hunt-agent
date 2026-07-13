package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/RikuShimoida/job-hunt-agent/internal/application"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/port"
)

func fixedNow() time.Time {
	return time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
}

func emailRaw(source, id, url string) model.RawJob {
	return model.RawJob{
		SourceName: source,
		Format:     "email",
		ExternalID: id,
		Body: "From: agent@example.test\n" +
			"Subject: 案件のご案内\n" +
			"\n" +
			"案件名: Java／AWS 基盤改善案件\n" +
			"企業: 架空テクノロジー\n" +
			"想定単価: 75〜85万円\n" +
			"リモート: フルリモート\n" +
			"稼働: 週3日\n" +
			"必須スキル: Java、Spring、AWS\n" +
			"URL: " + url + "\n",
	}
}

func searchingProfile() model.Profile {
	return model.Profile{
		SearchStatus:   model.SearchStatusSearching,
		RequiredSkills: []string{"Java"},
	}
}

func TestCollectSavesJobs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := newFakeRepository()
	conn := &fakeConnector{
		name: "fixture-email",
		raws: []model.RawJob{
			emailRaw("fixture-email", "1", "https://example.test/jobs/1"),
			emailRaw("fixture-email", "2", "https://example.test/jobs/2"),
		},
	}

	c := application.NewCollector(repo, []port.Connector{conn}, discardLogger(), fixedNow)

	summary, err := c.Collect(ctx, searchingProfile())
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}

	if summary.FetchedCount != 2 {
		t.Errorf("FetchedCount = %d, want 2", summary.FetchedCount)
	}
	if summary.NewCount != 2 {
		t.Errorf("NewCount = %d, want 2", summary.NewCount)
	}
	if repo.jobCount() != 2 {
		t.Errorf("保存件数 = %d, want 2", repo.jobCount())
	}
}

func TestCollectDoesNotDuplicateOnRerun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := newFakeRepository()
	conn := &fakeConnector{
		name: "fixture-email",
		raws: []model.RawJob{emailRaw("fixture-email", "1", "https://example.test/jobs/1")},
	}

	c := application.NewCollector(repo, []port.Connector{conn}, discardLogger(), fixedNow)

	first, err := c.Collect(ctx, searchingProfile())
	if err != nil {
		t.Fatalf("1回目の Collect() でエラー: %v", err)
	}
	if first.NewCount != 1 {
		t.Fatalf("1回目の NewCount = %d, want 1", first.NewCount)
	}

	second, err := c.Collect(ctx, searchingProfile())
	if err != nil {
		t.Fatalf("2回目の Collect() でエラー: %v", err)
	}
	if second.NewCount != 0 {
		t.Errorf("2回目の NewCount = %d, want 0", second.NewCount)
	}
	if second.DuplicateCount != 1 {
		t.Errorf("2回目の DuplicateCount = %d, want 1", second.DuplicateCount)
	}
	if repo.jobCount() != 1 {
		t.Errorf("再実行後の保存件数 = %d, want 1（重複登録されている）", repo.jobCount())
	}
}

func TestCollectContinuesWhenOneConnectorFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := newFakeRepository()

	failing := &fakeConnector{name: "broken", err: errConnectorFailed}
	healthy := &fakeConnector{
		name: "fixture-email",
		raws: []model.RawJob{emailRaw("fixture-email", "1", "https://example.test/jobs/1")},
	}

	c := application.NewCollector(repo,
		[]port.Connector{failing, healthy}, discardLogger(), fixedNow)

	summary, err := c.Collect(ctx, searchingProfile())
	if err != nil {
		t.Fatalf("Collect() returned error: %v（部分失敗で全体を落としてはならない）", err)
	}

	if repo.jobCount() != 1 {
		t.Errorf("保存件数 = %d, want 1（正常なコネクタの結果が保存されるべき）", repo.jobCount())
	}
	if len(summary.FailedSources) != 1 || summary.FailedSources[0] != "broken" {
		t.Errorf("FailedSources = %v, want [broken]", summary.FailedSources)
	}
	if healthy.callCount != 1 {
		t.Errorf("正常なコネクタの呼び出し回数 = %d, want 1", healthy.callCount)
	}

	runs := repo.runsFor("broken")
	if len(runs) != 1 {
		t.Fatalf("失敗ソースの実行履歴 = %d件, want 1件", len(runs))
	}
	if runs[0].Status != model.RunStatusFailed {
		t.Errorf("失敗ソースの Status = %q, want failed", runs[0].Status)
	}
	if runs[0].ErrorMessage == "" {
		t.Error("失敗ソースの ErrorMessage が空（原因を特定できない）")
	}
}

// TestCollectRecordsRunStatusOnSaveResult は、保存の成否が実行履歴へ
// 正しく反映されることを確かめる。
//
// 保存に失敗したのに status=success で履歴を書くと、サマリ（失敗）と
// 履歴（成功）が矛盾し、後から原因を追えなくなる。
func TestCollectRecordsRunStatusOnSaveResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		saveJobErr       error
		wantStatus       model.RunStatus
		wantErrorMessage bool
		wantNewCount     int
		wantFailedSource bool
	}{
		{
			name:             "保存が成功したソースは success で記録される",
			saveJobErr:       nil,
			wantStatus:       model.RunStatusSuccess,
			wantErrorMessage: false,
			wantNewCount:     1,
			wantFailedSource: false,
		},
		{
			name:             "保存に失敗したソースは failed と error_message で記録される",
			saveJobErr:       errSaveJobFailed,
			wantStatus:       model.RunStatusFailed,
			wantErrorMessage: true,
			wantNewCount:     0,
			wantFailedSource: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			repo := newFakeRepository()
			repo.saveJobErr = tt.saveJobErr

			conn := &fakeConnector{
				name: "fixture-email",
				raws: []model.RawJob{emailRaw("fixture-email", "1", "https://example.test/jobs/1")},
			}

			c := application.NewCollector(repo, []port.Connector{conn}, discardLogger(), fixedNow)

			summary, err := c.Collect(ctx, searchingProfile())
			if err != nil {
				t.Fatalf("Collect() returned error: %v（保存失敗で全体を落としてはならない）", err)
			}

			runs := repo.runsFor("fixture-email")
			if len(runs) != 1 {
				t.Fatalf("実行履歴 = %d件, want 1件", len(runs))
			}
			if runs[0].Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", runs[0].Status, tt.wantStatus)
			}

			hasMessage := runs[0].ErrorMessage != ""
			if hasMessage != tt.wantErrorMessage {
				t.Errorf("ErrorMessage = %q, want message: %v",
					runs[0].ErrorMessage, tt.wantErrorMessage)
			}
			if tt.wantErrorMessage &&
				!strings.Contains(runs[0].ErrorMessage, errSaveJobFailed.Error()) {
				t.Errorf("ErrorMessage = %q, want to contain %q",
					runs[0].ErrorMessage, errSaveJobFailed.Error())
			}

			if summary.NewCount != tt.wantNewCount {
				t.Errorf("NewCount = %d, want %d", summary.NewCount, tt.wantNewCount)
			}

			gotFailedSource := len(summary.FailedSources) > 0
			if gotFailedSource != tt.wantFailedSource {
				t.Errorf("FailedSources = %v, want failure: %v",
					summary.FailedSources, tt.wantFailedSource)
			}
		})
	}
}

// TestCollectContinuesWhenSaveFailsForOneSource は、保存失敗が
// 他ソースの収集を巻き込まないことを確かめる。
func TestCollectContinuesWhenSaveFailsForOneSource(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := newFakeRepository()
	repo.saveJobErr = errSaveJobFailed
	repo.saveJobErrSource = "broken"

	broken := &fakeConnector{
		name: "broken",
		raws: []model.RawJob{emailRaw("broken", "1", "https://example.test/jobs/broken")},
	}
	healthy := &fakeConnector{
		name: "fixture-email",
		raws: []model.RawJob{emailRaw("fixture-email", "2", "https://example.test/jobs/2")},
	}

	c := application.NewCollector(repo,
		[]port.Connector{broken, healthy}, discardLogger(), fixedNow)

	summary, err := c.Collect(ctx, searchingProfile())
	if err != nil {
		t.Fatalf("Collect() returned error: %v（部分失敗で全体を落としてはならない）", err)
	}

	if healthy.callCount != 1 {
		t.Errorf("正常なソースの呼び出し回数 = %d, want 1（保存失敗で後続が止まっている）",
			healthy.callCount)
	}
	if repo.jobCount() != 1 {
		t.Errorf("保存件数 = %d, want 1（正常なソースの案件は保存されるべき）", repo.jobCount())
	}
	if len(summary.FailedSources) != 1 || summary.FailedSources[0] != "broken" {
		t.Errorf("FailedSources = %v, want [broken]", summary.FailedSources)
	}

	brokenRuns := repo.runsFor("broken")
	if len(brokenRuns) != 1 || brokenRuns[0].Status != model.RunStatusFailed {
		t.Fatalf("失敗ソースの実行履歴 = %+v, want status=failed が1件", brokenRuns)
	}

	healthyRuns := repo.runsFor("fixture-email")
	if len(healthyRuns) != 1 || healthyRuns[0].Status != model.RunStatusSuccess {
		t.Fatalf("正常ソースの実行履歴 = %+v, want status=success が1件", healthyRuns)
	}
	if healthyRuns[0].NewCount != 1 {
		t.Errorf("正常ソースの NewCount = %d, want 1", healthyRuns[0].NewCount)
	}
}

func TestCollectSkipsWhenPaused(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := newFakeRepository()
	conn := &fakeConnector{
		name: "fixture-email",
		raws: []model.RawJob{emailRaw("fixture-email", "1", "https://example.test/jobs/1")},
	}

	c := application.NewCollector(repo, []port.Connector{conn}, discardLogger(), fixedNow)

	p := searchingProfile()
	p.SearchStatus = model.SearchStatusPaused

	summary, err := c.Collect(ctx, p)
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}

	if conn.callCount != 0 {
		t.Errorf("paused なのにコネクタが %d回呼ばれた, want 0回", conn.callCount)
	}
	if summary.FetchedCount != 0 || summary.NewCount != 0 {
		t.Errorf("paused なのに収集が行われた: %+v", summary)
	}
	if repo.jobCount() != 0 {
		t.Errorf("paused なのに %d件保存された", repo.jobCount())
	}
}

func TestCollectLogsSummary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	repo := newFakeRepository()
	failing := &fakeConnector{name: "broken", err: errConnectorFailed}
	healthy := &fakeConnector{
		name: "fixture-email",
		raws: []model.RawJob{emailRaw("fixture-email", "1", "https://example.test/jobs/1")},
	}

	c := application.NewCollector(repo,
		[]port.Connector{failing, healthy}, logger, fixedNow)

	if _, err := c.Collect(ctx, searchingProfile()); err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}

	entry := findLogEntry(t, buf.String(), "収集サマリ")

	assertLogNumber(t, entry, "fetched", 1)
	assertLogNumber(t, entry, "new", 1)
	assertLogNumber(t, entry, "duplicate", 0)

	failed, ok := entry["failed_sources"].([]any)
	if !ok || len(failed) != 1 || failed[0] != "broken" {
		t.Errorf("failed_sources = %v, want [broken]", entry["failed_sources"])
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
}

// findLogEntry は JSON ログ行から msg が一致するものを探す。
func findLogEntry(t *testing.T, logs, msg string) map[string]any {
	t.Helper()

	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("ログ行が JSON として読めない: %v (%s)", err, line)
		}
		if entry["msg"] == msg {
			return entry
		}
	}
	t.Fatalf("msg=%q のログが出力されていない:\n%s", msg, logs)
	return nil
}

func assertLogNumber(t *testing.T, entry map[string]any, key string, want float64) {
	t.Helper()

	got, ok := entry[key].(float64)
	if !ok {
		t.Errorf("ログに %q が数値として含まれていない: %v", key, entry[key])
		return
	}
	if got != want {
		t.Errorf("ログの %q = %v, want %v", key, got, want)
	}
}
