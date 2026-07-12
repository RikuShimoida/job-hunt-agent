package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
	"github.com/RikuShimoida/job-hunt-agent/internal/platform/database"
	"github.com/RikuShimoida/job-hunt-agent/internal/repository/sqlite"
)

func newRepo(t *testing.T) (*sqlite.Repository, *sql.DB) {
	t.Helper()

	dsn := filepath.Join(t.TempDir(), "test.db")
	db, err := database.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("failed to close database: %v", err)
		}
	})
	return sqlite.New(db), db
}

func sampleJob(dedupKey string) model.JobPosting {
	now := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	rateMin, rateMax := 750000, 850000
	days := 3

	return model.JobPosting{
		Title:           "Java／AWS 基盤改善案件",
		CompanyName:     "架空テクノロジー",
		RawText:         "本文",
		RateType:        model.RateTypeMonthly,
		RateMin:         &rateMin,
		RateMax:         &rateMax,
		Currency:        "JPY",
		WorkDaysMin:     &days,
		WorkDaysMax:     &days,
		RemoteType:      model.RemoteTypeFullRemote,
		StartDate:       &start,
		ContractType:    "フリーランス",
		RequiredSkills:  []string{"Java", "Spring", "AWS"},
		PreferredSkills: []string{"Docker"},
		Roles:           []string{"バックエンド"},
		SourceURL:       "https://example.test/jobs/1",
		FirstSeenAt:     now,
		LastSeenAt:      now,
		DedupKey:        dedupKey,
		ContentHash:     "hash-1",
		Status:          model.JobStatusNew,
		Sources: []model.JobSource{
			{SourceName: "fixture-email", ExternalID: "mail-1"},
		},
	}
}

func TestSaveJobPersistsAllFields(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, _ := newRepo(t)

	job := sampleJob("url:https://example.test/jobs/1")
	created, err := repo.SaveJob(ctx, &job)
	if err != nil {
		t.Fatalf("SaveJob() returned error: %v", err)
	}
	if !created {
		t.Fatal("created = false, want true (初回保存)")
	}

	jobs, err := repo.ListJobs(ctx)
	if err != nil {
		t.Fatalf("ListJobs() returned error: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("ListJobs() = %d件, want 1件", len(jobs))
	}

	got := jobs[0]
	if got.Title != job.Title {
		t.Errorf("Title = %q, want %q", got.Title, job.Title)
	}
	if got.RateType != model.RateTypeMonthly {
		t.Errorf("RateType = %q, want monthly", got.RateType)
	}
	if got.RateMin == nil || *got.RateMin != 750000 {
		t.Errorf("RateMin = %v, want 750000", got.RateMin)
	}
	if got.RemoteType != model.RemoteTypeFullRemote {
		t.Errorf("RemoteType = %q, want full_remote", got.RemoteType)
	}
	if len(got.RequiredSkills) != 3 {
		t.Errorf("RequiredSkills = %v, want 3件", got.RequiredSkills)
	}
	if got.StartDate == nil || got.StartDate.Format("2006-01-02") != "2026-09-01" {
		t.Errorf("StartDate = %v, want 2026-09-01", got.StartDate)
	}
	if len(got.Sources) != 1 || got.Sources[0].SourceName != "fixture-email" {
		t.Errorf("Sources = %+v, want fixture-email", got.Sources)
	}
}

func TestSaveJobKeepsNullFieldsNull(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, _ := newRepo(t)

	job := sampleJob("hash:no-rate")
	job.RateType = model.RateTypeUnknown
	job.RateMin = nil
	job.RateMax = nil
	job.StartDate = nil

	if _, err := repo.SaveJob(ctx, &job); err != nil {
		t.Fatalf("SaveJob() returned error: %v", err)
	}

	jobs, err := repo.ListJobs(ctx)
	if err != nil {
		t.Fatalf("ListJobs() returned error: %v", err)
	}
	got := jobs[0]

	if got.RateMin != nil || got.RateMax != nil {
		t.Errorf("RateMin/RateMax = %v/%v, want nil/nil", got.RateMin, got.RateMax)
	}
	if got.StartDate != nil {
		t.Errorf("StartDate = %v, want nil", got.StartDate)
	}
}

func TestSaveJobDoesNotDuplicateSameDedupKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, _ := newRepo(t)

	const key = "url:https://example.test/jobs/1"

	first := sampleJob(key)
	created, err := repo.SaveJob(ctx, &first)
	if err != nil {
		t.Fatalf("1回目の SaveJob() でエラー: %v", err)
	}
	if !created {
		t.Fatal("1回目の created = false, want true")
	}

	second := sampleJob(key)
	second.LastSeenAt = first.LastSeenAt.Add(24 * time.Hour)

	created, err = repo.SaveJob(ctx, &second)
	if err != nil {
		t.Fatalf("2回目の SaveJob() でエラー: %v", err)
	}
	if created {
		t.Error("2回目の created = true, want false（同一 dedup_key は新規登録しない）")
	}

	count, err := repo.CountJobs(ctx)
	if err != nil {
		t.Fatalf("CountJobs() returned error: %v", err)
	}
	if count != 1 {
		t.Errorf("案件件数 = %d, want 1（重複登録されている）", count)
	}

	jobs, err := repo.ListJobs(ctx)
	if err != nil {
		t.Fatalf("ListJobs() returned error: %v", err)
	}
	if !jobs[0].LastSeenAt.Equal(second.LastSeenAt) {
		t.Errorf("LastSeenAt = %v, want %v（再取得時に更新されるべき）",
			jobs[0].LastSeenAt, second.LastSeenAt)
	}
}

func TestSaveJobMergesSourcesForSameJob(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, _ := newRepo(t)

	const key = "url:https://example.test/jobs/1"

	fromEmail := sampleJob(key)
	fromEmail.Sources = []model.JobSource{{SourceName: "fixture-email", ExternalID: "mail-1"}}
	if _, err := repo.SaveJob(ctx, &fromEmail); err != nil {
		t.Fatalf("SaveJob() returned error: %v", err)
	}

	fromHTML := sampleJob(key)
	fromHTML.Sources = []model.JobSource{{SourceName: "fixture-html", ExternalID: "page-1"}}
	if _, err := repo.SaveJob(ctx, &fromHTML); err != nil {
		t.Fatalf("SaveJob() returned error: %v", err)
	}

	jobs, err := repo.ListJobs(ctx)
	if err != nil {
		t.Fatalf("ListJobs() returned error: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("案件 = %d件, want 1件", len(jobs))
	}
	if len(jobs[0].Sources) != 2 {
		t.Errorf("Sources = %d件, want 2件（別ソースからの紹介元は両方残す）: %+v",
			len(jobs[0].Sources), jobs[0].Sources)
	}
}

func TestSaveJobDoesNotDuplicateSameSource(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, _ := newRepo(t)

	const key = "url:https://example.test/jobs/1"

	for range 3 {
		job := sampleJob(key)
		if _, err := repo.SaveJob(ctx, &job); err != nil {
			t.Fatalf("SaveJob() returned error: %v", err)
		}
	}

	jobs, err := repo.ListJobs(ctx)
	if err != nil {
		t.Fatalf("ListJobs() returned error: %v", err)
	}
	if len(jobs[0].Sources) != 1 {
		t.Errorf("Sources = %d件, want 1件（同一ソースの再取得で紹介元は増やさない）",
			len(jobs[0].Sources))
	}
}

func TestUpdateScore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, _ := newRepo(t)

	job := sampleJob("url:https://example.test/jobs/1")
	if _, err := repo.SaveJob(ctx, &job); err != nil {
		t.Fatalf("SaveJob() returned error: %v", err)
	}

	job.Score = 92
	job.ScoreReasons = []string{"フルリモート", "希望単価以上"}
	job.RejectionReasons = []string{"Terraform は歓迎条件"}
	job.Status = model.JobStatusScored

	if err := repo.UpdateScore(ctx, &job); err != nil {
		t.Fatalf("UpdateScore() returned error: %v", err)
	}

	jobs, err := repo.ListJobs(ctx)
	if err != nil {
		t.Fatalf("ListJobs() returned error: %v", err)
	}
	got := jobs[0]

	if got.Score != 92 {
		t.Errorf("Score = %d, want 92", got.Score)
	}
	if len(got.ScoreReasons) != 2 {
		t.Errorf("ScoreReasons = %v, want 2件", got.ScoreReasons)
	}
	if len(got.RejectionReasons) != 1 {
		t.Errorf("RejectionReasons = %v, want 1件", got.RejectionReasons)
	}
	if got.Status != model.JobStatusScored {
		t.Errorf("Status = %q, want scored", got.Status)
	}
}

func TestSaveRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, db := newRepo(t)

	run := model.CollectionRun{
		SourceName:     "fixture-email",
		StartedAt:      time.Now(),
		FinishedAt:     time.Now(),
		Status:         model.RunStatusFailed,
		ErrorMessage:   "failed to read fixture dir",
		FetchedCount:   0,
		NewCount:       0,
		DuplicateCount: 0,
	}
	if err := repo.SaveRun(ctx, &run); err != nil {
		t.Fatalf("SaveRun() returned error: %v", err)
	}
	if run.ID == 0 {
		t.Error("SaveRun() が ID を設定していない")
	}

	var (
		name   string
		status string
		errMsg string
	)
	err := db.QueryRowContext(ctx,
		"SELECT source_name, status, error_message FROM collection_runs WHERE id = ?", run.ID).
		Scan(&name, &status, &errMsg)
	if err != nil {
		t.Fatalf("failed to read collection_runs: %v", err)
	}
	if name != "fixture-email" {
		t.Errorf("source_name = %q, want fixture-email", name)
	}
	if status != string(model.RunStatusFailed) {
		t.Errorf("status = %q, want failed", status)
	}
	if errMsg == "" {
		t.Error("error_message が空（失敗理由が残っていない）")
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "test.db")

	// 同じ DB を2回開いてもマイグレーションが二重適用されないこと。
	for range 2 {
		db, err := database.Open(ctx, dsn)
		if err != nil {
			t.Fatalf("database.Open() returned error: %v", err)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("failed to close database: %v", err)
		}
	}
}
