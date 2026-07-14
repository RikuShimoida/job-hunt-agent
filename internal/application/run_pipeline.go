package application

import (
	"context"
	"log/slog"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
)

// RunSummary は collect → score → notify を通した集計。
type RunSummary struct {
	Collect CollectSummary
	Score   ScoreSummary
	Notify  NotifySummary
}

// Pipeline は収集・採点・通知を順に実行する。
type Pipeline struct {
	collector *Collector
	scorer    *Scorer
	notifier  *Notifier
	logger    *slog.Logger
}

func NewPipeline(c *Collector, s *Scorer, n *Notifier, logger *slog.Logger) *Pipeline {
	return &Pipeline{collector: c, scorer: s, notifier: n, logger: logger}
}

// Run は collect → score → notify を順に実行する。
func (p *Pipeline) Run(ctx context.Context, profile model.Profile) (RunSummary, error) {
	var summary RunSummary

	collectSummary, err := p.collector.Collect(ctx, profile)
	if err != nil {
		return summary, err
	}
	summary.Collect = collectSummary

	scoreSummary, err := p.scorer.Score(ctx, profile)
	if err != nil {
		return summary, err
	}
	summary.Score = scoreSummary

	notifySummary, err := p.notifier.Notify(ctx, profile)
	if err != nil {
		return summary, err
	}
	summary.Notify = notifySummary

	p.logger.InfoContext(ctx, "実行サマリ",
		slog.Int("fetched", summary.Collect.FetchedCount),
		slog.Int("new", summary.Collect.NewCount),
		slog.Int("duplicate", summary.Collect.DuplicateCount),
		slog.Int("updated", summary.Collect.UpdatedCount),
		slog.Int("notified", summary.Notify.TargetCount),
		slog.Int("sent", summary.Notify.SentCount),
		slog.Any("failed_sources", summary.Collect.FailedSources))

	return summary, nil
}
