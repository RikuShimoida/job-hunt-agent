// Package port は application 層が外界へ出るための契約を定義する。
//
// Go の慣習ではインターフェースを利用側パッケージで定義するが、
// application 層が唯一の利用側であり、実装（connector / repository / notifier）は
// すべてこの契約に従う。契約を1箇所へ集約したほうが依存方向が読みやすいため、
// application 内に散らさず port として切り出している。
package port

import (
	"context"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
)

// Connector は案件ソース1つぶんの取得口。
type Connector interface {
	Name() string
	Fetch(ctx context.Context) ([]model.RawJob, error)
}

// SaveResult は SaveJob の結果。
type SaveResult struct {
	// Created は新規登録されたかどうか。
	Created bool
	// MaterialChanges は既存案件と衝突したときに検出した重要変更。
	// 新規登録時と、変更がなかったときは空。
	MaterialChanges []string
}

// Repository は案件・実行履歴・通知履歴の永続化。
type Repository interface {
	// SaveJob は案件を保存する。DedupKey が既存と衝突した場合は新規登録せず、
	// 内容を更新した上で重要変更の有無を返す。
	SaveJob(ctx context.Context, job *model.JobPosting) (SaveResult, error)

	ListJobs(ctx context.Context) ([]model.JobPosting, error)

	// UpdateScore はスコアリング結果を反映する。
	UpdateScore(ctx context.Context, job *model.JobPosting) error

	// UpdateStatus は案件の処理段階だけを更新する。
	UpdateStatus(ctx context.Context, jobID int64, status model.JobStatus) error

	SaveRun(ctx context.Context, run *model.CollectionRun) error

	SaveNotification(ctx context.Context, n *model.Notification) error

	// ListNotifiedJobIDs は送信に成功した通知の job_id → payload_hash を返す。
	// 同じ案件に複数の成功行がある場合は最新の payload_hash を返す。
	ListNotifiedJobIDs(ctx context.Context) (map[int64]string, error)
}

// NotifyItem は通知1件ぶんの入力。
//
// JobPosting だけを渡すと「新着」と「更新」を区別できない。
// 区別を JobPosting のフィールドに持たせると、永続化モデルへ通知都合の
// 状態が混ざるため、通知の入力としてここで包む。
type NotifyItem struct {
	Job model.JobPosting
	// Update は通知済みの案件に重要変更があって再通知することを示す。
	Update bool
}

// Notifier は案件通知の送信口。
type Notifier interface {
	// Name は notifications.channel に記録する送信先の名前。
	Name() string

	// Notify は items を送り、送信試行の記録を返す。成功・失敗のどちらも記録として返す。
	// 実際には送らない実装（dry-run）は nil を返し、通知済みとして記録させない。
	Notify(ctx context.Context, items []NotifyItem) ([]model.Notification, error)
}

// ErrorNotifier はソース取得失敗の通知口。案件通知とは別の宛先へ送れるよう分けている。
type ErrorNotifier interface {
	NotifyError(ctx context.Context, failures []model.SourceFailure) error
}
