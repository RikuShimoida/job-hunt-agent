package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/RikuShimoida/job-hunt-agent/internal/application"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
)

// scoredJob は採点済みの案件。閾値・通知済み管理の検証に使う。
func scoredJob(title string, score int) model.JobPosting {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	rateMin, rateMax := 750000, 850000

	return model.JobPosting{
		Title:          title,
		Score:          score,
		Status:         model.JobStatusScored,
		RateType:       model.RateTypeMonthly,
		RateMin:        &rateMin,
		RateMax:        &rateMax,
		RemoteType:     model.RemoteTypeFullRemote,
		StartDate:      &start,
		RequiredSkills: []string{"Java", "AWS"},
		DedupKey:       "url:https://example.test/" + title,
	}
}

// saveScoredJob は採点済み案件を保存し、採番された ID を返す。
func saveScoredJob(t *testing.T, repo *fakeRepository, title string, score int) int64 {
	t.Helper()

	job := scoredJob(title, score)
	if _, err := repo.SaveJob(context.Background(), &job); err != nil {
		t.Fatalf("SaveJob() returned error: %v", err)
	}
	return job.ID
}

func searchingOnly() model.Profile {
	return model.Profile{SearchStatus: model.SearchStatusSearching}
}

func TestNotifyThresholdBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     model.SearchStatus
		threshold  int
		jobScores  []int
		wantTarget int
	}{
		{
			name:       "searching では59点は通知されない（閾値60の直下）",
			status:     model.SearchStatusSearching,
			jobScores:  []int{59},
			wantTarget: 0,
		},
		{
			name:       "searching では60点は通知される（閾値60ちょうど）",
			status:     model.SearchStatusSearching,
			jobScores:  []int{60},
			wantTarget: 1,
		},
		{
			name:       "watching では79点は通知されない（閾値80の直下）",
			status:     model.SearchStatusWatching,
			jobScores:  []int{79},
			wantTarget: 0,
		},
		{
			name:       "watching では80点は通知される（閾値80ちょうど）",
			status:     model.SearchStatusWatching,
			jobScores:  []int{80},
			wantTarget: 1,
		},
		{
			name:       "watching では60点台の案件は通知されない",
			status:     model.SearchStatusWatching,
			jobScores:  []int{60, 70, 79},
			wantTarget: 0,
		},
		{
			name:       "searching では閾値以上の案件だけが通知される",
			status:     model.SearchStatusSearching,
			jobScores:  []int{45, 59, 60, 92},
			wantTarget: 2,
		},
		{
			name:       "明示指定した閾値が状態別の既定値より優先される",
			status:     model.SearchStatusSearching,
			threshold:  90,
			jobScores:  []int{60, 89, 90},
			wantTarget: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			repo := newFakeRepository()
			for i, score := range tt.jobScores {
				saveScoredJob(t, repo, string(rune('a'+i)), score)
			}

			notifier := &fakeNotifier{}
			n := application.NewNotifier(repo, notifier, discardLogger())

			p := model.Profile{
				SearchStatus:          tt.status,
				NotificationThreshold: tt.threshold,
			}

			summary, err := n.Notify(ctx, p)
			if err != nil {
				t.Fatalf("Notify() returned error: %v", err)
			}

			if summary.TargetCount != tt.wantTarget {
				t.Errorf("TargetCount = %d, want %d", summary.TargetCount, tt.wantTarget)
			}
			if summary.SentCount != tt.wantTarget {
				t.Errorf("SentCount = %d, want %d", summary.SentCount, tt.wantTarget)
			}
			if got := len(notifier.notifiedItems()); got != tt.wantTarget {
				t.Errorf("Notifier に渡された案件 = %d件, want %d件", got, tt.wantTarget)
			}
		})
	}
}

func TestNotifySkipsWhenPaused(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := newFakeRepository()
	saveScoredJob(t, repo, "満点案件", 100)

	notifier := &fakeNotifier{}
	n := application.NewNotifier(repo, notifier, discardLogger())

	summary, err := n.Notify(ctx, model.Profile{SearchStatus: model.SearchStatusPaused})
	if err != nil {
		t.Fatalf("Notify() returned error: %v", err)
	}

	if summary.TargetCount != 0 {
		t.Errorf("TargetCount = %d, want 0（paused では通知しない）", summary.TargetCount)
	}
	if notifier.callCount() != 0 {
		t.Errorf("paused なのに Notifier が %d回呼ばれた, want 0回", notifier.callCount())
	}
}

func TestNotifyExcludesRejectedJobs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := newFakeRepository()

	rejected := scoredJob("除外案件", 100)
	rejected.Status = model.JobStatusRejected
	if _, err := repo.SaveJob(ctx, &rejected); err != nil {
		t.Fatalf("SaveJob() returned error: %v", err)
	}

	notifier := &fakeNotifier{}
	n := application.NewNotifier(repo, notifier, discardLogger())

	summary, err := n.Notify(ctx, searchingOnly())
	if err != nil {
		t.Fatalf("Notify() returned error: %v", err)
	}

	if summary.TargetCount != 0 {
		t.Errorf("TargetCount = %d, want 0（除外済み案件は通知しない）", summary.TargetCount)
	}
}

func TestNotifySortsByScoreDescending(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := newFakeRepository()

	for _, score := range []int{65, 92, 78} {
		saveScoredJob(t, repo, string(rune('a'+score%26)), score)
	}

	notifier := &fakeNotifier{}
	n := application.NewNotifier(repo, notifier, discardLogger())

	if _, err := n.Notify(ctx, searchingOnly()); err != nil {
		t.Fatalf("Notify() returned error: %v", err)
	}

	got := notifier.notifiedItems()
	if len(got) != 3 {
		t.Fatalf("通知件数 = %d, want 3", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Job.Score < got[i].Job.Score {
			t.Errorf("スコア降順になっていない: %d件目=%d, %d件目=%d",
				i, got[i-1].Job.Score, i+1, got[i].Job.Score)
		}
	}
}

// TestNotifyDoesNotResendNotifiedJob は、2回目の通知で同じ案件が
// 送られないことを確かめる（通知済み管理）。
func TestNotifyDoesNotResendNotifiedJob(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := newFakeRepository()
	id := saveScoredJob(t, repo, "Java／AWS 基盤改善案件", 92)

	notifier := &fakeNotifier{}
	n := application.NewNotifier(repo, notifier, discardLogger())

	first, err := n.Notify(ctx, searchingOnly())
	if err != nil {
		t.Fatalf("1回目の Notify() でエラー: %v", err)
	}
	if first.SentCount != 1 {
		t.Fatalf("1回目の SentCount = %d, want 1", first.SentCount)
	}

	job, ok := repo.jobByID(id)
	if !ok {
		t.Fatal("案件が保存されていない")
	}
	if job.Status != model.JobStatusNotified {
		t.Errorf("Status = %q, want notified", job.Status)
	}

	notifier.reset()

	second, err := n.Notify(ctx, searchingOnly())
	if err != nil {
		t.Fatalf("2回目の Notify() でエラー: %v", err)
	}

	if second.TargetCount != 0 {
		t.Errorf("2回目の TargetCount = %d, want 0（同じ案件が再通知されている）",
			second.TargetCount)
	}
	if notifier.callCount() != 0 {
		t.Errorf("2回目に Notifier が %d回呼ばれた, want 0回", notifier.callCount())
	}
}

// TestNotifyResendsOnMaterialChange は、重要変更のあった案件が
// 「更新」として再通知されることを確かめる。
func TestNotifyResendsOnMaterialChange(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := newFakeRepository()
	id := saveScoredJob(t, repo, "Java／AWS 基盤改善案件", 92)

	notifier := &fakeNotifier{}
	n := application.NewNotifier(repo, notifier, discardLogger())

	if _, err := n.Notify(ctx, searchingOnly()); err != nil {
		t.Fatalf("1回目の Notify() でエラー: %v", err)
	}

	notifier.reset()
	repo.updateRate(id, 900000, 1000000)

	second, err := n.Notify(ctx, searchingOnly())
	if err != nil {
		t.Fatalf("2回目の Notify() でエラー: %v", err)
	}

	if second.TargetCount != 1 {
		t.Fatalf("2回目の TargetCount = %d, want 1（重要変更が再通知されていない）",
			second.TargetCount)
	}

	items := notifier.notifiedItems()
	if len(items) != 1 {
		t.Fatalf("再通知された案件 = %d件, want 1件", len(items))
	}
	if !items[0].Update {
		t.Error("Update = false, want true（既通知の案件は「更新」として送られるべき）")
	}
}

// TestNotifyDoesNotResendOnCosmeticChange は、加点理由の文言だけが変わっても
// 再通知されないことを確かめる（payload_hash は重要変更だけから導出する）。
func TestNotifyDoesNotResendOnCosmeticChange(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := newFakeRepository()
	id := saveScoredJob(t, repo, "Java／AWS 基盤改善案件", 92)

	notifier := &fakeNotifier{}
	n := application.NewNotifier(repo, notifier, discardLogger())

	if _, err := n.Notify(ctx, searchingOnly()); err != nil {
		t.Fatalf("1回目の Notify() でエラー: %v", err)
	}

	notifier.reset()
	repo.updateScoreReasons(id, []string{"希望単価以上（750000〜850000円）", "フルリモート"})

	second, err := n.Notify(ctx, searchingOnly())
	if err != nil {
		t.Fatalf("2回目の Notify() でエラー: %v", err)
	}

	if second.TargetCount != 0 {
		t.Errorf("2回目の TargetCount = %d, want 0（文言の変更で再通知されている）",
			second.TargetCount)
	}
}

// TestNotifyRetriesFailedJobOnNextRun は、送信に失敗した案件が notified にならず、
// 次回実行で再送されることを確かめる。
func TestNotifyRetriesFailedJobOnNextRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := newFakeRepository()
	id := saveScoredJob(t, repo, "Java／AWS 基盤改善案件", 92)

	notifier := &fakeNotifier{
		failTitles: map[string]struct{}{"Java／AWS 基盤改善案件": {}},
	}
	n := application.NewNotifier(repo, notifier, discardLogger())

	first, err := n.Notify(ctx, searchingOnly())
	if err != nil {
		t.Fatalf("1回目の Notify() でエラー: %v", err)
	}
	if first.FailedCount != 1 || first.SentCount != 0 {
		t.Fatalf("1回目 = sent %d / failed %d, want sent 0 / failed 1",
			first.SentCount, first.FailedCount)
	}

	job, ok := repo.jobByID(id)
	if !ok {
		t.Fatal("案件が保存されていない")
	}
	if job.Status == model.JobStatusNotified {
		t.Error("送信に失敗したのに Status = notified になっている")
	}

	records := repo.notificationsFor(id)
	if len(records) != 1 || records[0].Result != model.NotificationResultFailed {
		t.Fatalf("通知履歴 = %+v, want result=failed が1件", records)
	}

	notifier.reset()
	notifier.failTitles = nil

	second, err := n.Notify(ctx, searchingOnly())
	if err != nil {
		t.Fatalf("2回目の Notify() でエラー: %v", err)
	}
	if second.SentCount != 1 {
		t.Errorf("2回目の SentCount = %d, want 1（失敗した案件が再送されていない）",
			second.SentCount)
	}

	job, _ = repo.jobByID(id)
	if job.Status != model.JobStatusNotified {
		t.Errorf("再送後の Status = %q, want notified", job.Status)
	}
}

// TestNotifyPersistsSentJobsOnCancel は、送信途中で ctx がキャンセルされても
// 送信済みの案件が通知履歴に残り、notified になることを確かめる。
//
// 記録を送信と同じ ctx で行うと、中断時に「Slack には届いたのに記録されない」状態になり、
// 次回実行で同じ案件が再送される（重複通知）。
func TestNotifyPersistsSentJobsOnCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	repo := newFakeRepository()
	sent := saveScoredJob(t, repo, "送信済み案件", 92)
	unsent := saveScoredJob(t, repo, "未送信案件", 80)

	// 1件送った直後に中断させる。
	notifier := &fakeNotifier{cancel: cancel, cancelAfter: 1}
	n := application.NewNotifier(repo, notifier, discardLogger())

	summary, err := n.Notify(ctx, searchingOnly())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Notify() error = %v, want context.Canceled でラップされたエラー", err)
	}

	if summary.SentCount != 1 {
		t.Errorf("SentCount = %d, want 1", summary.SentCount)
	}

	records := repo.notificationsFor(sent)
	if len(records) != 1 || records[0].Result != model.NotificationResultSuccess {
		t.Fatalf("送信済み案件の通知履歴 = %+v, want result=success が1件（中断で記録が失われている）",
			records)
	}

	job, ok := repo.jobByID(sent)
	if !ok {
		t.Fatal("案件が保存されていない")
	}
	if job.Status != model.JobStatusNotified {
		t.Errorf("送信済み案件の Status = %q, want notified（次回実行で再送されてしまう）",
			job.Status)
	}

	// 送らなかった案件は通知済みにしない（次回実行で送る）。
	if got := repo.notificationsFor(unsent); len(got) != 0 {
		t.Errorf("未送信案件の通知履歴 = %+v, want 0件", got)
	}
	if job, _ := repo.jobByID(unsent); job.Status == model.JobStatusNotified {
		t.Error("送っていない案件が notified になっている")
	}
}

// TestNotifyConfirmsSuccessesOnPartialFailure は、一部の送信が失敗しても
// 成功した案件は notified として確定することを確かめる。
func TestNotifyConfirmsSuccessesOnPartialFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := newFakeRepository()

	okA := saveScoredJob(t, repo, "成功案件A", 92)
	ng := saveScoredJob(t, repo, "失敗案件", 88)
	okB := saveScoredJob(t, repo, "成功案件B", 70)

	notifier := &fakeNotifier{
		failTitles: map[string]struct{}{"失敗案件": {}},
	}
	n := application.NewNotifier(repo, notifier, discardLogger())

	summary, err := n.Notify(ctx, searchingOnly())
	if err != nil {
		t.Fatalf("Notify() returned error: %v（部分失敗で全体を落としてはならない）", err)
	}

	if summary.TargetCount != 3 {
		t.Errorf("TargetCount = %d, want 3", summary.TargetCount)
	}
	if summary.SentCount != 2 {
		t.Errorf("SentCount = %d, want 2", summary.SentCount)
	}
	if summary.FailedCount != 1 {
		t.Errorf("FailedCount = %d, want 1", summary.FailedCount)
	}

	for _, id := range []int64{okA, okB} {
		job, _ := repo.jobByID(id)
		if job.Status != model.JobStatusNotified {
			t.Errorf("成功した案件 %d の Status = %q, want notified", id, job.Status)
		}
	}

	failed, _ := repo.jobByID(ng)
	if failed.Status == model.JobStatusNotified {
		t.Error("失敗した案件が notified になっている")
	}
}
