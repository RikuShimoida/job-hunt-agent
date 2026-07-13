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
	// TargetCount は通知対象として送信口へ渡した件数。
	TargetCount int
	// SentCount は送信に成功した件数。dry-run は何も送らないため 0。
	SentCount int
	// FailedCount は送信に失敗した件数。次回実行で再送される。
	FailedCount int
}

// Notifier は閾値以上かつ未通知の案件を通知する。
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

	notified, err := n.repo.ListNotifiedJobIDs(ctx)
	if err != nil {
		return summary, fmt.Errorf("failed to list notified jobs: %w", err)
	}

	items := selectTargets(jobs, notified, p.Threshold())
	summary.TargetCount = len(items)

	records, notifyErr := n.notifier.Notify(ctx, items)

	// 送信途中で失敗しても、成功した案件はここで確定させる。
	// 失敗を理由に全件を未通知へ戻すと、送信済みの案件が次回も再送されて重複通知になる。
	//
	// 永続化だけ ctx のキャンセルから切り離すのは、Ctrl-C や SIGTERM で中断したときに
	// 「Slack には届いたのに記録されない」状態を作らないため。記録が残らないと
	// 次回実行で同じ案件が再送され、この PR の目的（重複通知を出さない）を自ら破る。
	// 送信側の ctx は元のまま渡し、中断で送信自体は止まるようにしている。
	persistCtx := context.WithoutCancel(ctx)
	for i := range records {
		n.persist(persistCtx, &records[i], &summary)
	}

	if notifyErr != nil {
		return summary, fmt.Errorf("failed to notify: %w", notifyErr)
	}

	n.logger.InfoContext(ctx, "通知サマリ",
		slog.Int("threshold", p.Threshold()),
		slog.Int("target", summary.TargetCount),
		slog.Int("sent", summary.SentCount),
		slog.Int("failed", summary.FailedCount))

	return summary, nil
}

// selectTargets は閾値以上で、未通知または重要変更のあった案件を選ぶ。
func selectTargets(jobs []model.JobPosting, notified map[int64]string, threshold int) []port.NotifyItem {
	items := make([]port.NotifyItem, 0, len(jobs))

	for _, job := range jobs {
		if job.Status == model.JobStatusRejected || job.Score < threshold {
			continue
		}

		prevHash, alreadyNotified := notified[job.ID]
		if alreadyNotified && prevHash == model.MaterialHash(job) {
			continue
		}
		items = append(items, port.NotifyItem{Job: job, Update: alreadyNotified})
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Job.Score > items[j].Job.Score
	})
	return items
}

// persist は送信試行を記録し、成功した案件を notified にする。
// 記録の失敗で通知全体を落とさない（送信自体は済んでおり、取り消せないため）。
func (n *Notifier) persist(ctx context.Context, rec *model.Notification, summary *NotifySummary) {
	if err := n.repo.SaveNotification(ctx, rec); err != nil {
		n.logger.ErrorContext(ctx, "通知履歴の保存に失敗しました",
			slog.Int64("job_id", rec.JobID),
			slog.String("error", err.Error()))
	}

	if rec.Result != model.NotificationResultSuccess {
		summary.FailedCount++
		n.logger.ErrorContext(ctx, "通知の送信に失敗しました",
			slog.Int64("job_id", rec.JobID),
			slog.String("error", rec.ErrorMessage))
		return
	}

	summary.SentCount++
	if err := n.repo.UpdateStatus(ctx, rec.JobID, model.JobStatusNotified); err != nil {
		n.logger.ErrorContext(ctx, "通知済みステータスの更新に失敗しました",
			slog.Int64("job_id", rec.JobID),
			slog.String("error", err.Error()))
	}
}
