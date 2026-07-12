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

// Repository は案件と実行履歴の永続化。
type Repository interface {
	// SaveJob は案件を保存する。DedupKey が既存と衝突した場合は
	// 新規登録せず LastSeenAt のみ更新し、created=false を返す。
	SaveJob(ctx context.Context, job *model.JobPosting) (created bool, err error)

	ListJobs(ctx context.Context) ([]model.JobPosting, error)

	// UpdateScore はスコアリング結果を反映する。
	UpdateScore(ctx context.Context, job *model.JobPosting) error

	SaveRun(ctx context.Context, run *model.CollectionRun) error
}

// Notifier は通知の送信口。
type Notifier interface {
	Notify(ctx context.Context, jobs []model.JobPosting) error
}
