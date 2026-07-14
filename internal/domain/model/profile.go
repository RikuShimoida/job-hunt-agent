package model

import "time"

// DefaultThresholdSearching / DefaultThresholdWatching は
// Profile.NotificationThreshold が未設定のときに使う状態別の既定閾値。
const (
	DefaultThresholdSearching = 60
	DefaultThresholdWatching  = 80
)

// Profile は利用者の希望条件。案件の除外判定とスコアリングの基準になる。
type Profile struct {
	SearchStatus SearchStatus `yaml:"search_status"`

	AvailableFrom *time.Time `yaml:"available_from"`

	MinimumRate int `yaml:"minimum_rate"`
	TargetRate  int `yaml:"target_rate"`

	PreferredWorkDays int `yaml:"preferred_work_days"`
	MonthlyHoursMin   int `yaml:"monthly_hours_min"`
	MonthlyHoursMax   int `yaml:"monthly_hours_max"`

	RemoteRequired   bool     `yaml:"remote_required"`
	MaxOnsiteDays    int      `yaml:"max_onsite_days"`
	AllowedLocations []string `yaml:"allowed_locations"`

	ContractTypes   []string `yaml:"contract_types"`
	RequiredSkills  []string `yaml:"required_skills"`
	PreferredSkills []string `yaml:"preferred_skills"`
	LearningSkills  []string `yaml:"learning_skills"`
	DesiredRoles    []string `yaml:"desired_roles"`

	ExcludedKeywords []string `yaml:"excluded_keywords"`

	// 0 は未設定を意味する。Threshold() が状態別の既定値へフォールバックする。
	NotificationThreshold int `yaml:"notification_threshold"`

	SkillSheetPath string `yaml:"skill_sheet_path"`
}

// Threshold は通知閾値を返す。NotificationThreshold が未設定（0）なら
// SearchStatus に応じた既定値を使う。
func (p Profile) Threshold() int {
	if p.NotificationThreshold > 0 {
		return p.NotificationThreshold
	}
	switch p.SearchStatus {
	case SearchStatusWatching:
		return DefaultThresholdWatching
	default:
		return DefaultThresholdSearching
	}
}

// NotifiesEnabled は現在の求職状態で通知を行うかを返す。
func (p Profile) NotifiesEnabled() bool {
	return p.SearchStatus != SearchStatusPaused
}

// CollectsEnabled は現在の求職状態で収集を行うかを返す。
func (p Profile) CollectsEnabled() bool {
	return p.SearchStatus != SearchStatusPaused
}
