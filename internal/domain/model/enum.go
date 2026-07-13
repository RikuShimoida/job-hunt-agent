package model

// SearchStatus は利用者の求職状態を表す。
type SearchStatus string

const (
	SearchStatusSearching SearchStatus = "searching"
	SearchStatusWatching  SearchStatus = "watching"
	SearchStatusPaused    SearchStatus = "paused"
)

func (s SearchStatus) Valid() bool {
	switch s {
	case SearchStatusSearching, SearchStatusWatching, SearchStatusPaused:
		return true
	default:
		return false
	}
}

// RemoteType は案件のリモート勤務形態を表す。
type RemoteType string

const (
	RemoteTypeFullRemote RemoteType = "full_remote"
	RemoteTypeHybrid     RemoteType = "hybrid"
	RemoteTypeOnsite     RemoteType = "onsite"
	RemoteTypeUnknown    RemoteType = "unknown"
)

func (r RemoteType) Valid() bool {
	switch r {
	case RemoteTypeFullRemote, RemoteTypeHybrid, RemoteTypeOnsite, RemoteTypeUnknown:
		return true
	default:
		return false
	}
}

// RateType は単価の単位を表す。
type RateType string

const (
	RateTypeMonthly RateType = "monthly"
	RateTypeHourly  RateType = "hourly"
	RateTypeUnknown RateType = "unknown"
)

func (r RateType) Valid() bool {
	switch r {
	case RateTypeMonthly, RateTypeHourly, RateTypeUnknown:
		return true
	default:
		return false
	}
}

// JobStatus は案件の処理段階を表す。
type JobStatus string

const (
	JobStatusNew      JobStatus = "new"
	JobStatusScored   JobStatus = "scored"
	JobStatusRejected JobStatus = "rejected"
	JobStatusNotified JobStatus = "notified"
)

// RunStatus は1回の収集実行の結果を表す。
type RunStatus string

const (
	RunStatusSuccess RunStatus = "success"
	RunStatusFailed  RunStatus = "failed"
)
