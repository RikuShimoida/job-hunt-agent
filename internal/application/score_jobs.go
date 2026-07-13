package application

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/port"
	"github.com/RikuShimoida/job-hunt-agent/internal/matching"
)

// ScoreSummary は1回の採点の集計。
type ScoreSummary struct {
	ScoredCount   int
	RejectedCount int
}

// Scorer は保存済み案件を再評価する。
type Scorer struct {
	repo   port.Repository
	logger *slog.Logger
}

func NewScorer(repo port.Repository, logger *slog.Logger) *Scorer {
	return &Scorer{repo: repo, logger: logger}
}

// Score は全案件を評価してスコアと理由を保存する。
func (s *Scorer) Score(ctx context.Context, p model.Profile) (ScoreSummary, error) {
	var summary ScoreSummary

	jobs, err := s.repo.ListJobs(ctx)
	if err != nil {
		return summary, fmt.Errorf("failed to list jobs: %w", err)
	}

	for i := range jobs {
		job := &jobs[i]
		result := matching.Evaluate(*job, p)

		job.Score = result.Score
		job.ScoreReasons = result.ScoreReasons
		job.RejectionReasons = result.RejectionReasons
		if result.Rejected {
			job.Status = model.JobStatusRejected
			summary.RejectedCount++
		} else {
			job.Status = model.JobStatusScored
			summary.ScoredCount++
		}

		if err := s.repo.UpdateScore(ctx, job); err != nil {
			return summary, fmt.Errorf("failed to update score: %w", err)
		}
	}

	s.logger.InfoContext(ctx, "採点サマリ",
		slog.Int("scored", summary.ScoredCount),
		slog.Int("rejected", summary.RejectedCount))

	return summary, nil
}
