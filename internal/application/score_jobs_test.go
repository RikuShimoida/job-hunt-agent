package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/RikuShimoida/job-hunt-agent/internal/application"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
)

func scoringProfile() model.Profile {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	return model.Profile{
		SearchStatus:     model.SearchStatusSearching,
		AvailableFrom:    &from,
		MinimumRate:      700000,
		TargetRate:       800000,
		RemoteRequired:   true,
		RequiredSkills:   []string{"Java"},
		PreferredSkills:  []string{"Spring", "AWS"},
		ExcludedKeywords: []string{"常駐必須"},
	}
}

func TestScoreAssignsScoresAndReasons(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := newFakeRepository()

	rateMin, rateMax := 800000, 850000
	good := model.JobPosting{
		Title:          "良案件",
		DedupKey:       "url:https://example.test/good",
		RateType:       model.RateTypeMonthly,
		RateMin:        &rateMin,
		RateMax:        &rateMax,
		RemoteType:     model.RemoteTypeFullRemote,
		RequiredSkills: []string{"Java", "Spring", "AWS"},
		Status:         model.JobStatusNew,
	}
	if _, err := repo.SaveJob(ctx, &good); err != nil {
		t.Fatalf("SaveJob() returned error: %v", err)
	}

	s := application.NewScorer(repo, discardLogger())

	summary, err := s.Score(ctx, scoringProfile())
	if err != nil {
		t.Fatalf("Score() returned error: %v", err)
	}

	if summary.ScoredCount != 1 {
		t.Errorf("ScoredCount = %d, want 1", summary.ScoredCount)
	}
	if summary.RejectedCount != 0 {
		t.Errorf("RejectedCount = %d, want 0", summary.RejectedCount)
	}

	jobs, err := repo.ListJobs(ctx)
	if err != nil {
		t.Fatalf("ListJobs() returned error: %v", err)
	}
	got := jobs[0]

	if got.Score <= 0 || got.Score > 100 {
		t.Errorf("Score = %d, want 1..100", got.Score)
	}
	if len(got.ScoreReasons) == 0 {
		t.Error("加点理由が保存されていない")
	}
	if got.Status != model.JobStatusScored {
		t.Errorf("Status = %q, want scored", got.Status)
	}
}

func TestScoreMarksRejectedJobs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := newFakeRepository()

	onsite := model.JobPosting{
		Title:      "常駐案件",
		DedupKey:   "url:https://example.test/onsite",
		RemoteType: model.RemoteTypeOnsite,
		Status:     model.JobStatusNew,
	}
	if _, err := repo.SaveJob(ctx, &onsite); err != nil {
		t.Fatalf("SaveJob() returned error: %v", err)
	}

	s := application.NewScorer(repo, discardLogger())

	summary, err := s.Score(ctx, scoringProfile())
	if err != nil {
		t.Fatalf("Score() returned error: %v", err)
	}

	if summary.RejectedCount != 1 {
		t.Errorf("RejectedCount = %d, want 1", summary.RejectedCount)
	}

	jobs, err := repo.ListJobs(ctx)
	if err != nil {
		t.Fatalf("ListJobs() returned error: %v", err)
	}
	got := jobs[0]

	if got.Status != model.JobStatusRejected {
		t.Errorf("Status = %q, want rejected", got.Status)
	}
	if got.Score != 0 {
		t.Errorf("除外案件の Score = %d, want 0", got.Score)
	}
	if len(got.RejectionReasons) == 0 {
		t.Error("除外理由が保存されていない")
	}
}
