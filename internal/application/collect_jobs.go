package application

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/RikuShimoida/job-hunt-agent/internal/deduplication"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/port"
	"github.com/RikuShimoida/job-hunt-agent/internal/parser"
	"github.com/RikuShimoida/job-hunt-agent/internal/parser/email"
	htmlparser "github.com/RikuShimoida/job-hunt-agent/internal/parser/html"
)

// CollectSummary は1回の収集の集計。
type CollectSummary struct {
	FetchedCount   int
	NewCount       int
	DuplicateCount int
	FailedSources  []string
}

// Collector は有効なコネクタから案件を収集して保存する。
type Collector struct {
	repo       port.Repository
	connectors []port.Connector
	logger     *slog.Logger
	now        func() time.Time
}

func NewCollector(repo port.Repository, connectors []port.Connector, logger *slog.Logger, now func() time.Time) *Collector {
	return &Collector{repo: repo, connectors: connectors, logger: logger, now: now}
}

// Collect は全コネクタを順に実行する。
// あるコネクタが失敗しても他コネクタの処理は継続し、失敗はサマリとログに残す。
func (c *Collector) Collect(ctx context.Context, p model.Profile) (CollectSummary, error) {
	var summary CollectSummary

	if !p.CollectsEnabled() {
		c.logger.InfoContext(ctx, "収集をスキップしました",
			slog.String("search_status", string(p.SearchStatus)))
		return summary, nil
	}

	for _, conn := range c.connectors {
		started := c.now()

		raws, err := conn.Fetch(ctx)
		if err != nil {
			summary.FailedSources = append(summary.FailedSources, conn.Name())
			c.logger.ErrorContext(ctx, "ソースの取得に失敗しました",
				slog.String("source", conn.Name()),
				slog.String("error", err.Error()))

			c.saveRun(ctx, &model.CollectionRun{
				SourceName:   conn.Name(),
				StartedAt:    started,
				FinishedAt:   c.now(),
				Status:       model.RunStatusFailed,
				ErrorMessage: err.Error(),
			})
			continue
		}

		jobs := make([]model.JobPosting, 0, len(raws))
		for _, raw := range raws {
			job, err := toJobPosting(raw, c.now())
			if err != nil {
				c.logger.WarnContext(ctx, "案件の解析に失敗しました",
					slog.String("source", conn.Name()),
					slog.String("external_id", raw.ExternalID),
					slog.String("error", err.Error()))
				continue
			}
			jobs = append(jobs, job)
		}

		deduped := deduplication.Dedupe(jobs)

		var newCount, dupCount int
		var saveErr error
		for i := range deduped.Jobs {
			created, err := c.repo.SaveJob(ctx, &deduped.Jobs[i])
			if err != nil {
				saveErr = err
				c.logger.ErrorContext(ctx, "案件の保存に失敗しました",
					slog.String("source", conn.Name()),
					slog.String("error", err.Error()))
				break
			}
			if created {
				newCount++
			} else {
				dupCount++
			}
		}

		// 保存に失敗しても、そこまでに保存できた件数はサマリへ残す
		// （実際に保存された件数と食い違わせないため）。
		summary.FetchedCount += len(raws)
		summary.NewCount += newCount
		summary.DuplicateCount += dupCount + deduped.DuplicateCount

		run := &model.CollectionRun{
			SourceName:     conn.Name(),
			StartedAt:      started,
			FinishedAt:     c.now(),
			Status:         model.RunStatusSuccess,
			FetchedCount:   len(raws),
			NewCount:       newCount,
			DuplicateCount: dupCount + deduped.DuplicateCount,
		}
		if saveErr != nil {
			summary.FailedSources = append(summary.FailedSources, conn.Name())
			run.Status = model.RunStatusFailed
			run.ErrorMessage = saveErr.Error()
		}
		c.saveRun(ctx, run)

		if saveErr != nil {
			continue
		}

		c.logger.InfoContext(ctx, "ソースの収集が完了しました",
			slog.String("source", conn.Name()),
			slog.Int("fetched", len(raws)),
			slog.Int("new", newCount),
			slog.Int("duplicate", dupCount+deduped.DuplicateCount))
	}

	c.logger.InfoContext(ctx, "収集サマリ",
		slog.Int("fetched", summary.FetchedCount),
		slog.Int("new", summary.NewCount),
		slog.Int("duplicate", summary.DuplicateCount),
		slog.Any("failed_sources", summary.FailedSources))

	return summary, nil
}

// saveRun は実行履歴の保存失敗で収集全体を落とさない（履歴は副次的な記録のため）。
func (c *Collector) saveRun(ctx context.Context, run *model.CollectionRun) {
	if err := c.repo.SaveRun(ctx, run); err != nil {
		c.logger.ErrorContext(ctx, "実行履歴の保存に失敗しました",
			slog.String("source", run.SourceName),
			slog.String("error", err.Error()))
	}
}

func toJobPosting(raw model.RawJob, now time.Time) (model.JobPosting, error) {
	switch raw.Format {
	case "email":
		header, fields := email.Parse(raw.Body)
		enriched := raw
		if enriched.Sender == "" {
			enriched.Sender = header.From
		}
		if header.MessageID != "" {
			enriched.ExternalID = header.MessageID
		}
		return parser.Build(enriched, fields, now), nil

	case "html":
		fields, err := htmlparser.Parse(raw.Body)
		if err != nil {
			return model.JobPosting{}, err
		}
		return parser.Build(raw, fields, now), nil

	default:
		return model.JobPosting{}, fmt.Errorf("unsupported raw job format %q", raw.Format)
	}
}
