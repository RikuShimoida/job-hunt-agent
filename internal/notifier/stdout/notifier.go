// Package stdout は通知内容を標準出力へ書く Notifier。
//
// Phase 1 の dry-run 用。Slack への実送信は Phase 2 で別実装として足す。
package stdout

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
)

// Notifier は通知内容を w へ書く。
type Notifier struct {
	w io.Writer
}

func New(w io.Writer) *Notifier {
	return &Notifier{w: w}
}

// Notify は案件を人が読める形式で出力する。
func (n *Notifier) Notify(ctx context.Context, jobs []model.JobPosting) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("notify canceled: %w", err)
	}

	if len(jobs) == 0 {
		if _, err := fmt.Fprintln(n.w, "通知対象の案件はありません。"); err != nil {
			return fmt.Errorf("failed to write notification: %w", err)
		}
		return nil
	}

	for _, job := range jobs {
		if _, err := fmt.Fprint(n.w, Format(job)); err != nil {
			return fmt.Errorf("failed to write notification: %w", err)
		}
	}
	return nil
}

// Format は1案件ぶんの通知テキストを組み立てる。
func Format(job model.JobPosting) string {
	var b strings.Builder

	fmt.Fprintf(&b, "【%d点・新着】%s\n", job.Score, job.Title)
	fmt.Fprintf(&b, "単価：%s　稼働：%s　開始：%s\n",
		formatRate(job), formatWorkDays(job), formatStart(job))
	fmt.Fprintf(&b, "勤務：%s　紹介元：%s\n", formatRemote(job), formatSources(job))

	if loc := strings.TrimSpace(job.Location); loc != "" {
		fmt.Fprintf(&b, "勤務地：%s\n", loc)
	}
	if skills := job.RequiredSkills; len(skills) > 0 {
		fmt.Fprintf(&b, "主要スキル：%s\n", strings.Join(skills, "、"))
	}
	if len(job.ScoreReasons) > 0 {
		fmt.Fprintf(&b, "加点：%s\n", strings.Join(job.ScoreReasons, "、"))
	}
	if len(job.RejectionReasons) > 0 {
		fmt.Fprintf(&b, "減点：%s\n", strings.Join(job.RejectionReasons, "、"))
	}
	if url := strings.TrimSpace(job.SourceURL); url != "" {
		fmt.Fprintf(&b, "URL：%s\n", url)
	}
	b.WriteString("\n")

	return b.String()
}

func formatRate(job model.JobPosting) string {
	if job.RateMin == nil || job.RateMax == nil {
		return "不明"
	}
	unit := "円"
	if job.RateType == model.RateTypeHourly {
		unit = "円/時"
	}
	if *job.RateMin == *job.RateMax {
		return fmt.Sprintf("%d%s", *job.RateMin, unit)
	}
	return fmt.Sprintf("%d〜%d%s", *job.RateMin, *job.RateMax, unit)
}

func formatWorkDays(job model.JobPosting) string {
	if job.WorkDaysMin == nil || job.WorkDaysMax == nil {
		return "不明"
	}
	if *job.WorkDaysMin == *job.WorkDaysMax {
		return fmt.Sprintf("週%d日", *job.WorkDaysMin)
	}
	return fmt.Sprintf("週%d〜%d日", *job.WorkDaysMin, *job.WorkDaysMax)
}

func formatStart(job model.JobPosting) string {
	if job.StartDate == nil {
		return "不明"
	}
	return job.StartDate.Format("2006-01-02")
}

func formatRemote(job model.JobPosting) string {
	switch job.RemoteType {
	case model.RemoteTypeFullRemote:
		return "フルリモート"
	case model.RemoteTypeHybrid:
		if job.OnsiteDays != nil {
			return fmt.Sprintf("ハイブリッド（週%d日出社）", *job.OnsiteDays)
		}
		return "ハイブリッド"
	case model.RemoteTypeOnsite:
		return "常駐"
	default:
		return "不明"
	}
}

func formatSources(job model.JobPosting) string {
	if len(job.Sources) == 0 {
		return "不明"
	}
	names := make([]string, 0, len(job.Sources))
	seen := make(map[string]struct{}, len(job.Sources))
	for _, s := range job.Sources {
		if _, dup := seen[s.SourceName]; dup {
			continue
		}
		seen[s.SourceName] = struct{}{}
		names = append(names, s.SourceName)
	}
	return strings.Join(names, "、")
}
