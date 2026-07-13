package application_test

import (
	"context"
	"testing"

	"github.com/RikuShimoida/job-hunt-agent/internal/application"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
)

// scoredJob はスコアだけを持つ採点済み案件。閾値の境界を検証するために使う。
func scoredJob(title string, score int) model.JobPosting {
	return model.JobPosting{
		Title:    title,
		Score:    score,
		Status:   model.JobStatusScored,
		DedupKey: "url:https://example.test/" + title,
	}
}

func TestNotifyThresholdBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		status      model.SearchStatus
		threshold   int
		jobScores   []int
		wantTitles  []string
		wantNotifed int
	}{
		{
			name:        "searching では59点は通知されない（閾値60の直下）",
			status:      model.SearchStatusSearching,
			jobScores:   []int{59},
			wantNotifed: 0,
		},
		{
			name:        "searching では60点は通知される（閾値60ちょうど）",
			status:      model.SearchStatusSearching,
			jobScores:   []int{60},
			wantNotifed: 1,
		},
		{
			name:        "watching では79点は通知されない（閾値80の直下）",
			status:      model.SearchStatusWatching,
			jobScores:   []int{79},
			wantNotifed: 0,
		},
		{
			name:        "watching では80点は通知される（閾値80ちょうど）",
			status:      model.SearchStatusWatching,
			jobScores:   []int{80},
			wantNotifed: 1,
		},
		{
			name:        "watching では60点台の案件は通知されない",
			status:      model.SearchStatusWatching,
			jobScores:   []int{60, 70, 79},
			wantNotifed: 0,
		},
		{
			name:        "searching では閾値以上の案件だけが通知される",
			status:      model.SearchStatusSearching,
			jobScores:   []int{45, 59, 60, 92},
			wantNotifed: 2,
		},
		{
			name:        "明示指定した閾値が状態別の既定値より優先される",
			status:      model.SearchStatusSearching,
			threshold:   90,
			jobScores:   []int{60, 89, 90},
			wantNotifed: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			repo := newFakeRepository()
			for i, score := range tt.jobScores {
				job := scoredJob(string(rune('a'+i)), score)
				if _, err := repo.SaveJob(ctx, &job); err != nil {
					t.Fatalf("SaveJob() returned error: %v", err)
				}
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

			if summary.NotifiedCount != tt.wantNotifed {
				t.Errorf("NotifiedCount = %d, want %d", summary.NotifiedCount, tt.wantNotifed)
			}
			if got := len(notifier.notifiedJobs()); got != tt.wantNotifed {
				t.Errorf("Notifier に渡された案件 = %d件, want %d件", got, tt.wantNotifed)
			}
		})
	}
}

func TestNotifySkipsWhenPaused(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := newFakeRepository()

	job := scoredJob("満点案件", 100)
	if _, err := repo.SaveJob(ctx, &job); err != nil {
		t.Fatalf("SaveJob() returned error: %v", err)
	}

	notifier := &fakeNotifier{}
	n := application.NewNotifier(repo, notifier, discardLogger())

	p := model.Profile{SearchStatus: model.SearchStatusPaused}

	summary, err := n.Notify(ctx, p)
	if err != nil {
		t.Fatalf("Notify() returned error: %v", err)
	}

	if summary.NotifiedCount != 0 {
		t.Errorf("NotifiedCount = %d, want 0（paused では通知しない）", summary.NotifiedCount)
	}
	if notifier.calls != 0 {
		t.Errorf("paused なのに Notifier が %d回呼ばれた, want 0回", notifier.calls)
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

	summary, err := n.Notify(ctx, model.Profile{SearchStatus: model.SearchStatusSearching})
	if err != nil {
		t.Fatalf("Notify() returned error: %v", err)
	}

	if summary.NotifiedCount != 0 {
		t.Errorf("NotifiedCount = %d, want 0（除外済み案件は通知しない）", summary.NotifiedCount)
	}
}

func TestNotifySortsByScoreDescending(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := newFakeRepository()

	for _, score := range []int{65, 92, 78} {
		job := scoredJob(string(rune('a'+score%26)), score)
		if _, err := repo.SaveJob(ctx, &job); err != nil {
			t.Fatalf("SaveJob() returned error: %v", err)
		}
	}

	notifier := &fakeNotifier{}
	n := application.NewNotifier(repo, notifier, discardLogger())

	if _, err := n.Notify(ctx, model.Profile{SearchStatus: model.SearchStatusSearching}); err != nil {
		t.Fatalf("Notify() returned error: %v", err)
	}

	got := notifier.notifiedJobs()
	if len(got) != 3 {
		t.Fatalf("通知件数 = %d, want 3", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Score < got[i].Score {
			t.Errorf("スコア降順になっていない: %d件目=%d, %d件目=%d",
				i, got[i-1].Score, i+1, got[i].Score)
		}
	}
}
