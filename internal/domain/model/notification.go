package model

import "time"

// Notification は送信済み通知の記録。
// Phase 1 では書き込まない（通知済み管理は Phase 2 で実装する）。
type Notification struct {
	ID          int64
	JobID       int64
	Channel     string
	SentAt      time.Time
	PayloadHash string
	Result      string
}
