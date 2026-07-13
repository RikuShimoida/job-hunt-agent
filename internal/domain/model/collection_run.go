package model

import "time"

// CollectionRun はソース1つぶんの収集結果。
// あるソースが失敗しても他ソースを継続するため、失敗も1レコードとして残す。
type CollectionRun struct {
	ID             int64
	SourceName     string
	StartedAt      time.Time
	FinishedAt     time.Time
	Status         RunStatus
	FetchedCount   int
	NewCount       int
	DuplicateCount int
	ErrorMessage   string
}
