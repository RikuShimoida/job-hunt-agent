package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
	"github.com/RikuShimoida/job-hunt-agent/internal/normalization"
)

const (
	defaultDatabaseURL = "./job-hunt-agent.db"
	defaultLogLevel    = "info"
)

// SourceType はコネクタの種別。
type SourceType string

const (
	SourceTypeFixtureEmail SourceType = "fixture_email"
	SourceTypeFixtureHTML  SourceType = "fixture_html"
	SourceTypeGmail        SourceType = "gmail"
)

const (
	defaultGmailNewerThan  = "30d"
	defaultGmailMaxResults = 100
)

// Source は案件ソース1つぶんの設定。
type Source struct {
	Name    string     `yaml:"name"`
	Type    SourceType `yaml:"type"`
	Enabled bool       `yaml:"enabled"`
	// Path は fixture 系ソースが読むディレクトリ。
	Path string `yaml:"path"`

	// Senders は gmail ソースが取得対象とする送信元アドレス。
	//
	// キーワード検索にしないのは、実受信箱では転職サイトの正社員求人・アルバイト求人が
	// 大量に混ざり、案件メールがノイズに埋もれるため。送信元で絞る。
	Senders []string `yaml:"senders"`
	// NewerThan は Gmail 検索の対象期間（例: "30d"）。空なら既定値。
	NewerThan string `yaml:"newer_than"`
	// MaxResults は1回の収集で取得するメールの上限。0 なら既定値。
	MaxResults int `yaml:"max_results"`
}

// GmailNewerThan は対象期間を返す（未設定なら既定値）。
func (s Source) GmailNewerThan() string {
	if s.NewerThan == "" {
		return defaultGmailNewerThan
	}
	return s.NewerThan
}

// GmailMaxResults は取得上限を返す（未設定なら既定値）。
func (s Source) GmailMaxResults() int {
	if s.MaxResults <= 0 {
		return defaultGmailMaxResults
	}
	return s.MaxResults
}

// Sources は sources.yaml のルート。
type Sources struct {
	Sources []Source `yaml:"sources"`
}

// Env は環境変数から読む設定。
type Env struct {
	DatabaseURL string
	LogLevel    string
	// SlackWebhookURL は案件通知の送信先。--dry-run なしの実送信で必須。
	SlackWebhookURL string
	// SlackErrorWebhookURL はソース取得失敗の送信先。未設定ならエラーは Slack へ送らない。
	SlackErrorWebhookURL string

	// Google* は Gmail の読み取りに使う。gmail ソースが有効なときのみ必須。
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRefreshToken string
}

// HasGoogleCredentials は Gmail の取得に必要な3つが揃っているかを返す。
func (e Env) HasGoogleCredentials() bool {
	return e.GoogleClientID != "" && e.GoogleClientSecret != "" && e.GoogleRefreshToken != ""
}

// LoadProfile は profile.yaml を読み、検証まで行う。
func LoadProfile(path string) (model.Profile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return model.Profile{}, fmt.Errorf("failed to read profile %s: %w", path, err)
	}
	return parseAndValidateProfile(b)
}

// parseAndValidateProfile は profile の YAML を unmarshal・正規化・検証する。
//
// 起動時ロード（LoadProfile）向けに**非 strict**でデコードする。既存 profile.yaml が
// 将来キーや手書きの余剰キーを持っていても起動を止めないため。
func parseAndValidateProfile(b []byte) (model.Profile, error) {
	var p model.Profile
	if err := yaml.Unmarshal(b, &p); err != nil {
		return p, fmt.Errorf("failed to parse profile: %w", err)
	}
	return normalizeAndValidateProfile(p)
}

// parseAndValidateProfileStrict は未知キーを拒否する strict デコードで検証する。
//
// apply（インタビュー更新）専用。apply が profile.yaml への唯一の書き込み経路になった以上、
// キー名のタイポ（`remote_requird` など）を黙って無視して no-op 保存するのは用途と噛み合わない。
// 起動時ロード（LoadProfile）は後方互換のため非 strict のまま（既存ファイルを止めない）。
func parseAndValidateProfileStrict(b []byte) (model.Profile, error) {
	var p model.Profile
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		// 空ドキュメント（io.EOF）は required_skills 未設定として検証で弾く（起動時と同じ扱い）。
		if errors.Is(err, io.EOF) {
			return normalizeAndValidateProfile(model.Profile{})
		}
		return p, fmt.Errorf("%w: %w", model.ErrInvalidProfile, err)
	}
	return normalizeAndValidateProfile(p)
}

func normalizeAndValidateProfile(p model.Profile) (model.Profile, error) {
	normalizeSkills(&p)
	if err := ValidateProfile(p); err != nil {
		return p, err
	}
	return p, nil
}

// normalizeSkills はプロフィール側のスキル・役割を案件側と同じ正規名へ寄せる。
//
// 照合時に小文字化するだけでは足りないのは、案件側が normalization.Skills() で
// 「k8s → Kubernetes」まで寄せられており、プロフィールを素通しにすると
// 同じスキルが一致せず加点が落ちるため。
func normalizeSkills(p *model.Profile) {
	p.RequiredSkills = normalization.Skills(p.RequiredSkills)
	p.PreferredSkills = normalization.Skills(p.PreferredSkills)
	p.LearningSkills = normalization.Skills(p.LearningSkills)
	p.DesiredRoles = normalization.Skills(p.DesiredRoles)
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
	if err := validateApplicationProfile(p.Application); err != nil {
		return err
	}
	return nil
}

// validateApplicationProfile は応募返信メール用セクションを検証する。
//
// セクション全体が空でも通す（後方互換。既存 profile.yaml はこのセクションを持たない）。
// 個々の文字列に長さ制限を課さないのは、良い記述を恣意的な閾値で誤って弾かないため。
// 検証するのは strengths の空要素だけ——`- ` だけの行のような YAML の書き崩れを
// 起動時に気づけるようにする（空文字は下書き素材にならない）。
func validateApplicationProfile(a model.ApplicationProfile) error {
	for i, s := range a.Strengths {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("%w: application.strengths[%d] must not be empty",
				model.ErrInvalidProfile, i)
		}
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

		// path の必須チェックを type ごとに分けるのは、gmail が読むのが
		// ディレクトリではなく受信箱であり、path を持たないため。
		switch src.Type {
		case SourceTypeFixtureEmail, SourceTypeFixtureHTML:
			if src.Path == "" {
				return fmt.Errorf("%w: source %q requires path", model.ErrInvalidSource, src.Name)
			}
		case SourceTypeGmail:
			if err := validateGmailSource(src); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: source %q has unsupported type %q",
				model.ErrInvalidSource, src.Name, src.Type)
		}
	}
	return nil
}

// gmailNewerThanRe は Gmail の newer_than が受け付ける形式（1d / 2w / 3m / 1y）。
var gmailNewerThanRe = regexp.MustCompile(`^[1-9][0-9]*[dwmy]$`)

// validateGmailSource は gmail ソース固有の設定を検証する。
//
// 形式の誤りを起動時に弾くのは、Gmail が不正なクエリをエラーにせず
// **0件で返す**ため。`newer_than: 30days` と書き間違えると、収集は成功扱いのまま
// 案件が1件も取れず、無音の原因を追うのが難しくなる。
func validateGmailSource(src Source) error {
	senders := 0
	for _, s := range src.Senders {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("%w: source %q has an empty sender", model.ErrInvalidSource, src.Name)
		}
		senders++
	}
	if senders == 0 {
		return fmt.Errorf("%w: source %q requires senders", model.ErrInvalidSource, src.Name)
	}

	if src.NewerThan != "" && !gmailNewerThanRe.MatchString(src.NewerThan) {
		return fmt.Errorf("%w: source %q has invalid newer_than %q (例: 30d / 2w / 3m / 1y)",
			model.ErrInvalidSource, src.Name, src.NewerThan)
	}
	if src.MaxResults < 0 {
		return fmt.Errorf("%w: source %q has negative max_results (%d)",
			model.ErrInvalidSource, src.Name, src.MaxResults)
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
		DatabaseURL:          os.Getenv("DATABASE_URL"),
		LogLevel:             os.Getenv("LOG_LEVEL"),
		SlackWebhookURL:      os.Getenv("SLACK_WEBHOOK_URL"),
		SlackErrorWebhookURL: os.Getenv("SLACK_ERROR_WEBHOOK_URL"),
		GoogleClientID:       os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret:   os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRefreshToken:   os.Getenv("GOOGLE_REFRESH_TOKEN"),
	}
	if e.DatabaseURL == "" {
		e.DatabaseURL = defaultDatabaseURL
	}
	if e.LogLevel == "" {
		e.LogLevel = defaultLogLevel
	}
	return e
}
