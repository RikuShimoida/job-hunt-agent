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
