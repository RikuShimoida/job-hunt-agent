package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
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
	result, err := repo.SaveJob(ctx, &job)
	if err != nil {
		t.Fatalf("SaveJob() returned error: %v", err)
	}
	if !result.Created {
		t.Fatal("Created = false, want true (初回保存)")
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
	result, err := repo.SaveJob(ctx, &first)
	if err != nil {
		t.Fatalf("1回目の SaveJob() でエラー: %v", err)
	}
	if !result.Created {
		t.Fatal("1回目の Created = false, want true")
	}

	second := sampleJob(key)
	second.LastSeenAt = first.LastSeenAt.Add(24 * time.Hour)

	result, err = repo.SaveJob(ctx, &second)
	if err != nil {
		t.Fatalf("2回目の SaveJob() でエラー: %v", err)
	}
	if result.Created {
		t.Error("2回目の Created = true, want false（同一 dedup_key は新規登録しない）")
	}
	if len(result.MaterialChanges) != 0 {
		t.Errorf("MaterialChanges = %v, want 空（内容が同じなら重要変更なし）",
			result.MaterialChanges)
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

// TestSaveJobUpdatesFieldsAndDetectsMaterialChange は、再収集で単価が変わったときに
// 重要変更として検出され、DB の値も更新されることを確かめる。
//
// 衝突時に last_seen_at だけを更新すると、単価が上がっても DB は古い値のまま残る。
func TestSaveJobUpdatesFieldsAndDetectsMaterialChange(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, _ := newRepo(t)

	const key = "url:https://example.test/jobs/1"

	first := sampleJob(key)
	if _, err := repo.SaveJob(ctx, &first); err != nil {
		t.Fatalf("1回目の SaveJob() でエラー: %v", err)
	}

	raisedMin, raisedMax := 900000, 1000000
	second := sampleJob(key)
	second.RateMin = &raisedMin
	second.RateMax = &raisedMax

	result, err := repo.SaveJob(ctx, &second)
	if err != nil {
		t.Fatalf("2回目の SaveJob() でエラー: %v", err)
	}

	if result.Created {
		t.Error("Created = true, want false（同一 dedup_key は新規登録しない）")
	}
	if len(result.MaterialChanges) != 1 {
		t.Fatalf("MaterialChanges = %v, want 1件（単価変更）", result.MaterialChanges)
	}
	if !strings.Contains(result.MaterialChanges[0], "単価") {
		t.Errorf("MaterialChanges = %q, want 単価の変更", result.MaterialChanges[0])
	}

	jobs, err := repo.ListJobs(ctx)
	if err != nil {
		t.Fatalf("ListJobs() returned error: %v", err)
	}
	got := jobs[0]

	if got.RateMin == nil || *got.RateMin != raisedMin {
		t.Errorf("RateMin = %v, want %d（DB が更新されていない）", got.RateMin, raisedMin)
	}
	if got.RateMax == nil || *got.RateMax != raisedMax {
		t.Errorf("RateMax = %v, want %d（DB が更新されていない）", got.RateMax, raisedMax)
	}
}

// TestSaveJobKeepsScoreOnUpdate は、再収集が採点結果を巻き戻さないことを確かめる。
// 収集は score の領分（status / score / 理由）を触らない。
func TestSaveJobKeepsScoreOnUpdate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, _ := newRepo(t)

	const key = "url:https://example.test/jobs/1"

	job := sampleJob(key)
	if _, err := repo.SaveJob(ctx, &job); err != nil {
		t.Fatalf("SaveJob() returned error: %v", err)
	}

	job.Score = 92
	job.ScoreReasons = []string{"フルリモート"}
	job.Status = model.JobStatusNotified
	if err := repo.UpdateScore(ctx, &job); err != nil {
		t.Fatalf("UpdateScore() returned error: %v", err)
	}

	recollected := sampleJob(key)
	recollected.LastSeenAt = job.LastSeenAt.Add(24 * time.Hour)
	if _, err := repo.SaveJob(ctx, &recollected); err != nil {
		t.Fatalf("再収集の SaveJob() でエラー: %v", err)
	}

	jobs, err := repo.ListJobs(ctx)
	if err != nil {
		t.Fatalf("ListJobs() returned error: %v", err)
	}
	got := jobs[0]

	if got.Score != 92 {
		t.Errorf("Score = %d, want 92（再収集で採点結果が巻き戻っている）", got.Score)
	}
	if got.Status != model.JobStatusNotified {
		t.Errorf("Status = %q, want notified（再収集で通知済みが巻き戻っている）", got.Status)
	}
	if !got.FirstSeenAt.Equal(job.FirstSeenAt) {
		t.Errorf("FirstSeenAt = %v, want %v（初回検出日時は保持されるべき）",
			got.FirstSeenAt, job.FirstSeenAt)
	}
}

func TestUpdateStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, _ := newRepo(t)

	job := sampleJob("url:https://example.test/jobs/1")
	if _, err := repo.SaveJob(ctx, &job); err != nil {
		t.Fatalf("SaveJob() returned error: %v", err)
	}

	if err := repo.UpdateStatus(ctx, job.ID, model.JobStatusNotified); err != nil {
		t.Fatalf("UpdateStatus() returned error: %v", err)
	}

	jobs, err := repo.ListJobs(ctx)
	if err != nil {
		t.Fatalf("ListJobs() returned error: %v", err)
	}
	if jobs[0].Status != model.JobStatusNotified {
		t.Errorf("Status = %q, want notified", jobs[0].Status)
	}
}

// TestNotificationsRoundTrip は、通知の送信試行が記録され、
// 成功した通知だけが通知済みとして引けることを確かめる。
func TestNotificationsRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, _ := newRepo(t)

	job := sampleJob("url:https://example.test/jobs/1")
	if _, err := repo.SaveJob(ctx, &job); err != nil {
		t.Fatalf("SaveJob() returned error: %v", err)
	}

	sentAt := time.Date(2026, 7, 13, 9, 0, 0, 0, time.UTC)

	// 1回目は失敗。失敗行を通知済みとして扱うと、次回実行で再送されず取りこぼす。
	failed := model.Notification{
		JobID:        job.ID,
		Channel:      "slack",
		SentAt:       sentAt,
		PayloadHash:  "hash-old",
		Result:       model.NotificationResultFailed,
		ErrorMessage: "slack send failed: status 500",
	}
	if err := repo.SaveNotification(ctx, &failed); err != nil {
		t.Fatalf("SaveNotification() returned error: %v", err)
	}
	if failed.ID == 0 {
		t.Error("SaveNotification() が ID を設定していない")
	}

	notified, err := repo.ListNotifiedJobIDs(ctx)
	if err != nil {
		t.Fatalf("ListNotifiedJobIDs() returned error: %v", err)
	}
	if _, ok := notified[job.ID]; ok {
		t.Error("送信に失敗した案件が通知済みとして返っている")
	}

	// 2回目は成功。
	succeeded := model.Notification{
		JobID:       job.ID,
		Channel:     "slack",
		SentAt:      sentAt.Add(time.Hour),
		PayloadHash: "hash-1",
		Result:      model.NotificationResultSuccess,
	}
	if err := repo.SaveNotification(ctx, &succeeded); err != nil {
		t.Fatalf("SaveNotification() returned error: %v", err)
	}

	notified, err = repo.ListNotifiedJobIDs(ctx)
	if err != nil {
		t.Fatalf("ListNotifiedJobIDs() returned error: %v", err)
	}
	if notified[job.ID] != "hash-1" {
		t.Errorf("payload_hash = %q, want hash-1", notified[job.ID])
	}

	// 重要変更で再通知したら、最新の payload_hash が返る。
	updated := model.Notification{
		JobID:       job.ID,
		Channel:     "slack",
		SentAt:      sentAt.Add(2 * time.Hour),
		PayloadHash: "hash-2",
		Result:      model.NotificationResultSuccess,
	}
	if err := repo.SaveNotification(ctx, &updated); err != nil {
		t.Fatalf("SaveNotification() returned error: %v", err)
	}

	notified, err = repo.ListNotifiedJobIDs(ctx)
	if err != nil {
		t.Fatalf("ListNotifiedJobIDs() returned error: %v", err)
	}
	if notified[job.ID] != "hash-2" {
		t.Errorf("payload_hash = %q, want hash-2（最新の成功を返すべき）", notified[job.ID])
	}
}

// TestListNotifiedJobIDsOrdersByID は、sent_at が巻き戻っても最後に保存した
// payload_hash が返ることを確かめる。
//
// sent_at はアプリ側の時刻をテキストで保存しており、タイムゾーンや時刻同期で
// 辞書順が保存順と食い違いうる。sent_at で並べると古い hash が最新として残り、
// 変更済みの案件が「通知済み・変更なし」と誤判定されて再通知されない。
func TestListNotifiedJobIDsOrdersByID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, _ := newRepo(t)

	job := sampleJob("url:https://example.test/jobs/1")
	if _, err := repo.SaveJob(ctx, &job); err != nil {
		t.Fatalf("SaveJob() returned error: %v", err)
	}

	sentAt := time.Date(2026, 7, 13, 9, 0, 0, 0, time.UTC)

	// 先に保存した行のほうが sent_at は新しい（時刻が巻き戻ったケース）。
	for _, n := range []model.Notification{
		{JobID: job.ID, Channel: "slack", SentAt: sentAt.Add(time.Hour), PayloadHash: "hash-old", Result: model.NotificationResultSuccess},
		{JobID: job.ID, Channel: "slack", SentAt: sentAt, PayloadHash: "hash-new", Result: model.NotificationResultSuccess},
	} {
		if err := repo.SaveNotification(ctx, &n); err != nil {
			t.Fatalf("SaveNotification() returned error: %v", err)
		}
	}

	notified, err := repo.ListNotifiedJobIDs(ctx)
	if err != nil {
		t.Fatalf("ListNotifiedJobIDs() returned error: %v", err)
	}
	if notified[job.ID] != "hash-new" {
		t.Errorf("payload_hash = %q, want hash-new（最後に保存した行を返すべき）", notified[job.ID])
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
