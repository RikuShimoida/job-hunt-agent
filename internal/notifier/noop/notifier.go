// Package noop は何も送らない通知実装。
//
// SLACK_ERROR_WEBHOOK_URL 未設定を「エラー通知なし」の正常系として扱うため、
// bootstrap 側で nil 判定を書かずに済むよう空実装を挿す。
package noop

import (
	"context"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
)

// ErrorNotifier は失敗を捨てる。ソース失敗は Collector が構造化ログへ残す。
type ErrorNotifier struct{}

func NewErrorNotifier() *ErrorNotifier { return &ErrorNotifier{} }

func (n *ErrorNotifier) NotifyError(context.Context, []model.SourceFailure) error { return nil }
