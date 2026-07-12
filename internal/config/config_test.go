package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/RikuShimoida/job-hunt-agent/internal/config"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
)

func validProfile() model.Profile {
	return model.Profile{
		SearchStatus:      model.SearchStatusSearching,
		MinimumRate:       700000,
		TargetRate:        850000,
		PreferredWorkDays: 3,
		MonthlyHoursMin:   100,
		MonthlyHoursMax:   160,
		RemoteRequired:    true,
		MaxOnsiteDays:     0,
		RequiredSkills:    []string{"Java"},
	}
}

func TestValidateProfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(p *model.Profile)
		wantErr bool
	}{
		{
			name:    "妥当なプロフィールは通る",
			mutate:  func(*model.Profile) {},
			wantErr: false,
		},
		{
			name:    "search_status が3値以外なら不正",
			mutate:  func(p *model.Profile) { p.SearchStatus = "looking" },
			wantErr: true,
		},
		{
			name:    "search_status が空なら不正",
			mutate:  func(p *model.Profile) { p.SearchStatus = "" },
			wantErr: true,
		},
		{
			name:    "minimum_rate が target_rate を超えると不正",
			mutate:  func(p *model.Profile) { p.MinimumRate = 900000 },
			wantErr: true,
		},
		{
			name:    "minimum_rate と target_rate が同じなら通る",
			mutate:  func(p *model.Profile) { p.MinimumRate = 850000 },
			wantErr: false,
		},
		{
			name:    "単価が負なら不正",
			mutate:  func(p *model.Profile) { p.MinimumRate = -1 },
			wantErr: true,
		},
		{
			name:    "稼働日数が8日以上なら不正",
			mutate:  func(p *model.Profile) { p.PreferredWorkDays = 8 },
			wantErr: true,
		},
		{
			name:    "稼働日数が7日なら通る",
			mutate:  func(p *model.Profile) { p.PreferredWorkDays = 7 },
			wantErr: false,
		},
		{
			name:    "月間稼働の min が max を超えると不正",
			mutate:  func(p *model.Profile) { p.MonthlyHoursMin = 200 },
			wantErr: true,
		},
		{
			name: "remote_required と max_onsite_days は両立しない",
			mutate: func(p *model.Profile) {
				p.RemoteRequired = true
				p.MaxOnsiteDays = 1
			},
			wantErr: true,
		},
		{
			name: "remote_required が false なら出社日数を許容する",
			mutate: func(p *model.Profile) {
				p.RemoteRequired = false
				p.MaxOnsiteDays = 2
			},
			wantErr: false,
		},
		{
			name:    "必須スキルが空なら不正",
			mutate:  func(p *model.Profile) { p.RequiredSkills = nil },
			wantErr: true,
		},
		{
			name:    "通知閾値が101なら不正",
			mutate:  func(p *model.Profile) { p.NotificationThreshold = 101 },
			wantErr: true,
		},
		{
			name:    "通知閾値が100なら通る",
			mutate:  func(p *model.Profile) { p.NotificationThreshold = 100 },
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := validProfile()
			tt.mutate(&p)

			err := config.ValidateProfile(p)
			if tt.wantErr {
				if err == nil {
					t.Fatal("エラーを期待したが nil だった")
				}
				if !errors.Is(err, model.ErrInvalidProfile) {
					t.Errorf("errors.Is(err, ErrInvalidProfile) = false, err = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("エラーを期待していないが返った: %v", err)
			}
		})
	}
}

func TestLoadProfileReturnsSentinelOnInvalidYAML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "profile.yaml")

	// search_status が不正な設定を書き、センチネルエラーで判別できることを確かめる。
	body := "search_status: looking\nrequired_skills:\n  - Java\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("failed to write profile: %v", err)
	}

	_, err := config.LoadProfile(path)
	if err == nil {
		t.Fatal("エラーを期待したが nil だった")
	}
	if !errors.Is(err, model.ErrInvalidProfile) {
		t.Errorf("errors.Is(err, ErrInvalidProfile) = false, err = %v", err)
	}
}

func TestValidateSources(t *testing.T) {
	t.Parallel()

	valid := config.Sources{Sources: []config.Source{
		{Name: "fixture-email", Type: config.SourceTypeFixtureEmail, Enabled: true, Path: "testdata/emails"},
	}}

	tests := []struct {
		name    string
		sources config.Sources
		wantErr bool
	}{
		{name: "妥当なソースは通る", sources: valid, wantErr: false},
		{
			name:    "ソースが0件なら不正",
			sources: config.Sources{},
			wantErr: true,
		},
		{
			name: "名前が空なら不正",
			sources: config.Sources{Sources: []config.Source{
				{Type: config.SourceTypeFixtureEmail, Path: "testdata/emails"},
			}},
			wantErr: true,
		},
		{
			name: "名前が重複していたら不正",
			sources: config.Sources{Sources: []config.Source{
				{Name: "dup", Type: config.SourceTypeFixtureEmail, Path: "a"},
				{Name: "dup", Type: config.SourceTypeFixtureHTML, Path: "b"},
			}},
			wantErr: true,
		},
		{
			name: "未対応の type なら不正",
			sources: config.Sources{Sources: []config.Source{
				{Name: "gmail", Type: "gmail", Path: "x"},
			}},
			wantErr: true,
		},
		{
			name: "path が空なら不正",
			sources: config.Sources{Sources: []config.Source{
				{Name: "fixture-email", Type: config.SourceTypeFixtureEmail},
			}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := config.ValidateSources(tt.sources)
			if tt.wantErr {
				if err == nil {
					t.Fatal("エラーを期待したが nil だった")
				}
				if !errors.Is(err, model.ErrInvalidSource) {
					t.Errorf("errors.Is(err, ErrInvalidSource) = false, err = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("エラーを期待していないが返った: %v", err)
			}
		})
	}
}

func TestSourcesFindReturnsErrUnknownSource(t *testing.T) {
	t.Parallel()

	s := config.Sources{Sources: []config.Source{
		{Name: "fixture-email", Type: config.SourceTypeFixtureEmail, Path: "testdata/emails"},
	}}

	if _, err := s.Find("fixture-email"); err != nil {
		t.Fatalf("既存ソースの検索でエラー: %v", err)
	}

	_, err := s.Find("gmail")
	if !errors.Is(err, model.ErrUnknownSource) {
		t.Errorf("errors.Is(err, ErrUnknownSource) = false, err = %v", err)
	}
}

func TestSourcesEnabled(t *testing.T) {
	t.Parallel()

	s := config.Sources{Sources: []config.Source{
		{Name: "a", Enabled: true},
		{Name: "b", Enabled: false},
		{Name: "c", Enabled: true},
	}}

	got := s.Enabled()
	if len(got) != 2 {
		t.Fatalf("Enabled() = %d件, want 2件", len(got))
	}
	if got[0].Name != "a" || got[1].Name != "c" {
		t.Errorf("Enabled() = %v, want [a c]", got)
	}
}

func TestProfileThreshold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		status    model.SearchStatus
		threshold int
		want      int
	}{
		{name: "searching の既定値は60", status: model.SearchStatusSearching, threshold: 0, want: 60},
		{name: "watching の既定値は80", status: model.SearchStatusWatching, threshold: 0, want: 80},
		{name: "明示指定があれば既定値を上書きする", status: model.SearchStatusSearching, threshold: 75, want: 75},
		{name: "watching でも明示指定が優先される", status: model.SearchStatusWatching, threshold: 50, want: 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := model.Profile{SearchStatus: tt.status, NotificationThreshold: tt.threshold}
			if got := p.Threshold(); got != tt.want {
				t.Errorf("Threshold() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestProfilePausedDisablesCollectAndNotify(t *testing.T) {
	t.Parallel()

	p := model.Profile{SearchStatus: model.SearchStatusPaused}

	if p.CollectsEnabled() {
		t.Error("paused なのに CollectsEnabled() が true")
	}
	if p.NotifiesEnabled() {
		t.Error("paused なのに NotifiesEnabled() が true")
	}
}
