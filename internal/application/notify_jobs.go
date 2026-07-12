package application

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/port"
)

// NotifySummary は1回の通知の集計。
type NotifySummary struct {
	NotifiedCount int
}

// Notifier は閾値以上の案件を通知する。
type Notifier struct {
	repo     port.Repository
	notifier port.Notifier
	logger   *slog.Logger
}

func NewNotifier(repo port.Repository, n port.Notifier, logger *slog.Logger) *Notifier {
	return &Notifier{repo: repo, notifier: n, logger: logger}
}

// Notify は通知対象を絞り込んで送る。
// paused のときは1件も送らない。除外済み案件も送らない。
func (n *Notifier) Notify(ctx context.Context, p model.Profile) (NotifySummary, error) {
	var summary NotifySummary

	if !p.NotifiesEnabled() {
		n.logger.InfoContext(ctx, "通知をスキップしました",
			slog.String("search_status", string(p.SearchStatus)))
		return summary, nil
	}

	jobs, err := n.repo.ListJobs(ctx)
	if err != nil {
		return summary, fmt.Errorf("failed to list jobs: %w", err)
	}

	threshold := p.Threshold()
	targets := make([]model.JobPosting, 0, len(jobs))
	for _, job := range jobs {
		if job.Status == model.JobStatusRejected {
			continue
		}
		if job.Score < threshold {
			continue
		}
		targets = append(targets, job)
	}

	sort.SliceStable(targets, func(i, j int) bool {
		return targets[i].Score > targets[j].Score
	})

	if err := n.notifier.Notify(ctx, targets); err != nil {
		return summary, fmt.Errorf("failed to notify: %w", err)
	}

	summary.NotifiedCount = len(targets)
	n.logger.InfoContext(ctx, "通知サマリ",
		slog.Int("threshold", threshold),
		slog.Int("notified", summary.NotifiedCount))

	return summary, nil
}
