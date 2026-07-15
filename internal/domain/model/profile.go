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

	Application ApplicationProfile `yaml:"application"`
}

// ApplicationProfile は応募返信メールの下書きを作るための本人情報。
//
// Profile 本体（希望条件）と別の構造体に切るのは、これがスコアリング・除外判定の
// 入力になってはならないため。同じ Profile に平置きすると matching が誤って読む余地が
// 残るが、別型にすれば型で経路を塞げる。job-hunt-agent（Go）は Gmail へ書き込まず、
// 下書きは claude.ai の Gmail コネクタが本セクションを素材として読むだけ。
type ApplicationProfile struct {
	Introduction       string   `yaml:"introduction"`
	CareerSummary      string   `yaml:"career_summary"`
	Strengths          []string `yaml:"strengths"`
	MotivationTemplate string   `yaml:"motivation_template"`
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
