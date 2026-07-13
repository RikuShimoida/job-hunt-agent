// Package stdout は通知内容を標準出力へ書く Notifier。--dry-run 用。
package stdout

import (
	"context"
	"fmt"
	"io"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/port"
	"github.com/RikuShimoida/job-hunt-agent/internal/notifier/message"
)

// Notifier は通知内容を w へ書く。
type Notifier struct {
	w io.Writer
}

func New(w io.Writer) *Notifier {
	return &Notifier{w: w}
}

func (n *Notifier) Name() string { return "stdout" }

// Notify は案件を人が読める形式で出力する。
//
// 送信記録を返さないのは、dry-run を通知済みとして記録すると、
// その後の実送信で「送ったつもりで送られていない」案件が生まれるため。
func (n *Notifier) Notify(ctx context.Context, items []port.NotifyItem) ([]model.Notification, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("notify canceled: %w", err)
	}

	if len(items) == 0 {
		if _, err := fmt.Fprintln(n.w, "通知対象の案件はありません。"); err != nil {
			return nil, fmt.Errorf("failed to write notification: %w", err)
		}
		return nil, nil
	}

	for _, item := range items {
		if _, err := fmt.Fprint(n.w, message.Format(item)); err != nil {
			return nil, fmt.Errorf("failed to write notification: %w", err)
		}
	}
	return nil, nil
}

// ErrorNotifier はソース取得失敗を w へ書く。
type ErrorNotifier struct {
	w io.Writer
}

func NewErrorNotifier(w io.Writer) *ErrorNotifier {
	return &ErrorNotifier{w: w}
}

func (n *ErrorNotifier) NotifyError(ctx context.Context, failures []model.SourceFailure) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("notify canceled: %w", err)
	}
	if len(failures) == 0 {
		return nil
	}
	if _, err := fmt.Fprint(n.w, message.FormatFailures(failures)); err != nil {
		return fmt.Errorf("failed to write error notification: %w", err)
	}
	return nil
}
