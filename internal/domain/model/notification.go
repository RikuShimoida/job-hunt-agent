package model

import "time"

// NotificationResult は通知1件の送信結果。
type NotificationResult string

const (
	NotificationResultSuccess NotificationResult = "success"
	NotificationResultFailed  NotificationResult = "failed"
)

// Notification は通知の送信試行1件ぶんの記録。
//
// 成功だけでなく失敗も1行として append する。成功だけを残すと
// 「失敗して再送された案件」の履歴が追えなくなるため、UNIQUE 制約は張らない。
type Notification struct {
	ID          int64
	JobID       int64
	Channel     string
	SentAt      time.Time
	PayloadHash string
	// MaterialFields は通知時点の重要変更項目のスナップショット（表示用）。
	// PayloadHash と別に持つのは、ハッシュからは「何がどう変わったか」を復元できず、
	// 旧値が notify の時点で job_postings から消えているため。
	MaterialFields []string
	Result         NotificationResult
	// ErrorMessage には Webhook URL を含めない（ログ・DB への秘密情報の漏洩を防ぐため）。
	ErrorMessage string
}
