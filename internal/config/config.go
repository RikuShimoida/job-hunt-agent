package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
)

const (
	defaultDatabaseURL = "./job-hunt-agent.db"
	defaultLogLevel    = "info"
)

// SourceType はコネクタの種別。Phase 1 では fixture のみ。
type SourceType string

const (
	SourceTypeFixtureEmail SourceType = "fixture_email"
	SourceTypeFixtureHTML  SourceType = "fixture_html"
)

// Source は案件ソース1つぶんの設定。
type Source struct {
	Name    string     `yaml:"name"`
	Type    SourceType `yaml:"type"`
	Enabled bool       `yaml:"enabled"`
	// Path は fixture 系ソースが読むディレクトリ。
	Path string `yaml:"path"`
}

// Sources は sources.yaml のルート。
type Sources struct {
	Sources []Source `yaml:"sources"`
}

// Env は環境変数から読む設定。
type Env struct {
	DatabaseURL string
	LogLevel    string
}

// LoadProfile は profile.yaml を読み、検証まで行う。
func LoadProfile(path string) (model.Profile, error) {
	var p model.Profile

	b, err := os.ReadFile(path)
	if err != nil {
		return p, fmt.Errorf("failed to read profile %s: %w", path, err)
	}
	if err := yaml.Unmarshal(b, &p); err != nil {
		return p, fmt.Errorf("failed to parse profile %s: %w", path, err)
	}
	if err := ValidateProfile(p); err != nil {
		return p, err
	}
	return p, nil
}

// ValidateProfile はプロフィールの整合性を検証する。
// 違反はすべて model.ErrInvalidProfile でラップし、errors.Is で判別可能にする。
func ValidateProfile(p model.Profile) error {
	if !p.SearchStatus.Valid() {
		return fmt.Errorf("%w: search_status %q must be one of searching/watching/paused",
			model.ErrInvalidProfile, p.SearchStatus)
	}
	if p.MinimumRate < 0 {
		return fmt.Errorf("%w: minimum_rate must not be negative", model.ErrInvalidProfile)
	}
	if p.TargetRate < 0 {
		return fmt.Errorf("%w: target_rate must not be negative", model.ErrInvalidProfile)
	}
	if p.TargetRate > 0 && p.MinimumRate > p.TargetRate {
		return fmt.Errorf("%w: minimum_rate (%d) must not exceed target_rate (%d)",
			model.ErrInvalidProfile, p.MinimumRate, p.TargetRate)
	}
	if p.PreferredWorkDays < 0 || p.PreferredWorkDays > 7 {
		return fmt.Errorf("%w: preferred_work_days (%d) must be between 0 and 7",
			model.ErrInvalidProfile, p.PreferredWorkDays)
	}
	if p.MonthlyHoursMin < 0 || p.MonthlyHoursMax < 0 {
		return fmt.Errorf("%w: monthly hours must not be negative", model.ErrInvalidProfile)
	}
	if p.MonthlyHoursMax > 0 && p.MonthlyHoursMin > p.MonthlyHoursMax {
		return fmt.Errorf("%w: monthly_hours_min (%d) must not exceed monthly_hours_max (%d)",
			model.ErrInvalidProfile, p.MonthlyHoursMin, p.MonthlyHoursMax)
	}
	if p.RemoteRequired && p.MaxOnsiteDays > 0 {
		return fmt.Errorf("%w: remote_required is true but max_onsite_days is %d",
			model.ErrInvalidProfile, p.MaxOnsiteDays)
	}
	if p.MaxOnsiteDays < 0 || p.MaxOnsiteDays > 7 {
		return fmt.Errorf("%w: max_onsite_days (%d) must be between 0 and 7",
			model.ErrInvalidProfile, p.MaxOnsiteDays)
	}
	if len(p.RequiredSkills) == 0 {
		return fmt.Errorf("%w: required_skills must not be empty", model.ErrInvalidProfile)
	}
	if p.NotificationThreshold < 0 || p.NotificationThreshold > 100 {
		return fmt.Errorf("%w: notification_threshold (%d) must be between 0 and 100",
			model.ErrInvalidProfile, p.NotificationThreshold)
	}
	return nil
}

// LoadSources は sources.yaml を読み、検証まで行う。
func LoadSources(path string) (Sources, error) {
	var s Sources

	b, err := os.ReadFile(path)
	if err != nil {
		return s, fmt.Errorf("failed to read sources %s: %w", path, err)
	}
	if err := yaml.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("failed to parse sources %s: %w", path, err)
	}
	if err := ValidateSources(s); err != nil {
		return s, err
	}
	return s, nil
}

// ValidateSources はソース設定の整合性を検証する。
func ValidateSources(s Sources) error {
	if len(s.Sources) == 0 {
		return fmt.Errorf("%w: at least one source is required", model.ErrInvalidSource)
	}
	seen := make(map[string]struct{}, len(s.Sources))
	for _, src := range s.Sources {
		if src.Name == "" {
			return fmt.Errorf("%w: source name must not be empty", model.ErrInvalidSource)
		}
		if _, dup := seen[src.Name]; dup {
			return fmt.Errorf("%w: duplicate source name %q", model.ErrInvalidSource, src.Name)
		}
		seen[src.Name] = struct{}{}

		switch src.Type {
		case SourceTypeFixtureEmail, SourceTypeFixtureHTML:
		default:
			return fmt.Errorf("%w: source %q has unsupported type %q",
				model.ErrInvalidSource, src.Name, src.Type)
		}
		if src.Path == "" {
			return fmt.Errorf("%w: source %q requires path", model.ErrInvalidSource, src.Name)
		}
	}
	return nil
}

// Enabled は有効なソースだけを返す。
func (s Sources) Enabled() []Source {
	out := make([]Source, 0, len(s.Sources))
	for _, src := range s.Sources {
		if src.Enabled {
			out = append(out, src)
		}
	}
	return out
}

// Find は名前でソースを引く。
func (s Sources) Find(name string) (Source, error) {
	for _, src := range s.Sources {
		if src.Name == name {
			return src, nil
		}
	}
	return Source{}, fmt.Errorf("%w: %q", model.ErrUnknownSource, name)
}

// LoadEnv は環境変数を読む。未設定なら既定値を使う。
func LoadEnv() Env {
	e := Env{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		LogLevel:    os.Getenv("LOG_LEVEL"),
	}
	if e.DatabaseURL == "" {
		e.DatabaseURL = defaultDatabaseURL
	}
	if e.LogLevel == "" {
		e.LogLevel = defaultLogLevel
	}
	return e
}
