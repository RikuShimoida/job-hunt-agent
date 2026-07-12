package normalization_test

import (
	"testing"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
	"github.com/RikuShimoida/job-hunt-agent/internal/normalization"
)

func TestRate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantType model.RateType
		wantMin  *int
		wantMax  *int
	}{
		{
			name:     "万円単位の範囲表記を月額へ変換する",
			input:    "75〜85万円",
			wantType: model.RateTypeMonthly,
			wantMin:  ptr(750000),
			wantMax:  ptr(850000),
		},
		{
			name:     "万円単位の単一値は min と max が同じになる",
			input:    "80万円",
			wantType: model.RateTypeMonthly,
			wantMin:  ptr(800000),
			wantMax:  ptr(800000),
		},
		{
			name:     "カンマ区切りの円表記を月額へ変換する",
			input:    "750,000円",
			wantType: model.RateTypeMonthly,
			wantMin:  ptr(750000),
			wantMax:  ptr(750000),
		},
		{
			name:     "時給表記は RateTypeHourly になる",
			input:    "5,000円/時",
			wantType: model.RateTypeHourly,
			wantMin:  ptr(5000),
			wantMax:  ptr(5000),
		},
		{
			name:     "時給と明示された万円表記も hourly になる",
			input:    "時給 1万円",
			wantType: model.RateTypeHourly,
			wantMin:  ptr(10000),
			wantMax:  ptr(10000),
		},
		{
			name:     "空文字は unknown で nil を返す",
			input:    "",
			wantType: model.RateTypeUnknown,
			wantMin:  nil,
			wantMax:  nil,
		},
		{
			name:     "数値を含まない文言は unknown で nil を返す",
			input:    "応相談",
			wantType: model.RateTypeUnknown,
			wantMin:  nil,
			wantMax:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotType, gotMin, gotMax := normalization.Rate(tt.input)
			if gotType != tt.wantType {
				t.Errorf("RateType = %q, want %q", gotType, tt.wantType)
			}
			assertIntPtr(t, "min", gotMin, tt.wantMin)
			assertIntPtr(t, "max", gotMax, tt.wantMax)
		})
	}
}

func TestWorkDays(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantMin *int
		wantMax *int
	}{
		{name: "週3日", input: "週3日", wantMin: ptr(3), wantMax: ptr(3)},
		{name: "週3〜4日の範囲", input: "週3〜4日", wantMin: ptr(3), wantMax: ptr(4)},
		{name: "週4-5日のハイフン表記", input: "週4-5日", wantMin: ptr(4), wantMax: ptr(5)},
		{name: "空文字は nil", input: "", wantMin: nil, wantMax: nil},
		{name: "稼働日数が書かれていない場合は nil", input: "応相談", wantMin: nil, wantMax: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotMin, gotMax := normalization.WorkDays(tt.input)
			assertIntPtr(t, "min", gotMin, tt.wantMin)
			assertIntPtr(t, "max", gotMax, tt.wantMax)
		})
	}
}

func TestMonthlyHours(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantMin *int
		wantMax *int
	}{
		{name: "140〜180時間", input: "140〜180時間", wantMin: ptr(140), wantMax: ptr(180)},
		{name: "100-140時間のハイフン表記", input: "100-140時間", wantMin: ptr(100), wantMax: ptr(140)},
		{name: "範囲でない場合は nil", input: "160時間", wantMin: nil, wantMax: nil},
		{name: "空文字は nil", input: "", wantMin: nil, wantMax: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotMin, gotMax := normalization.MonthlyHours(tt.input)
			assertIntPtr(t, "min", gotMin, tt.wantMin)
			assertIntPtr(t, "max", gotMax, tt.wantMax)
		})
	}
}

func TestRemote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      string
		wantType   model.RemoteType
		wantOnsite *int
	}{
		{name: "フルリモート", input: "フルリモート", wantType: model.RemoteTypeFullRemote, wantOnsite: nil},
		{name: "完全リモート", input: "完全リモート", wantType: model.RemoteTypeFullRemote, wantOnsite: nil},
		{name: "常駐必須は onsite", input: "常駐必須", wantType: model.RemoteTypeOnsite, wantOnsite: nil},
		{name: "リモート可は hybrid", input: "リモート可", wantType: model.RemoteTypeHybrid, wantOnsite: nil},
		{
			name:       "週1出社は hybrid で出社日数を取る",
			input:      "リモート可（週1出社）",
			wantType:   model.RemoteTypeHybrid,
			wantOnsite: ptr(1),
		},
		{
			name:       "週2日出社も hybrid で出社日数を取る",
			input:      "週2日出社",
			wantType:   model.RemoteTypeHybrid,
			wantOnsite: ptr(2),
		},
		{name: "空文字は unknown", input: "", wantType: model.RemoteTypeUnknown, wantOnsite: nil},
		{name: "判別できない文言は unknown", input: "応相談", wantType: model.RemoteTypeUnknown, wantOnsite: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotType, gotOnsite := normalization.Remote(tt.input)
			if gotType != tt.wantType {
				t.Errorf("RemoteType = %q, want %q", gotType, tt.wantType)
			}
			assertIntPtr(t, "onsite_days", gotOnsite, tt.wantOnsite)
		})
	}
}

func TestSkills(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{
			name:  "略称を正規名へ寄せる",
			input: []string{"JS", "TS", "AWS CDK"},
			want:  []string{"JavaScript", "TypeScript", "CDK"},
		},
		{
			name:  "正規化後に重複したものは1つにまとめる",
			input: []string{"JS", "JavaScript", "js"},
			want:  []string{"JavaScript"},
		},
		{
			name:  "別名テーブルに無い語はそのまま残す",
			input: []string{"Java", "Snowflake", "架空フレームワーク"},
			want:  []string{"Java", "Snowflake", "架空フレームワーク"},
		},
		{
			name:  "空要素は落とす",
			input: []string{"Java", "", "  "},
			want:  []string{"Java"},
		},
		{
			name:  "空スライスは nil",
			input: nil,
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := normalization.Skills(tt.input)
			assertStrings(t, got, tt.want)
		})
	}
}

func TestSplitList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "読点区切り", input: "Java、Spring、AWS", want: []string{"Java", "Spring", "AWS"}},
		{name: "カンマ区切り", input: "Java,Spring,AWS", want: []string{"Java", "Spring", "AWS"}},
		{name: "スラッシュ区切り", input: "Java / Spring", want: []string{"Java", "Spring"}},
		{name: "区切り文字の混在", input: "Java、Spring / AWS,Docker", want: []string{"Java", "Spring", "AWS", "Docker"}},
		{name: "空文字は nil", input: "", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := normalization.SplitList(tt.input)
			assertStrings(t, got, tt.want)
		})
	}
}

func ptr(v int) *int { return &v }

func assertIntPtr(t *testing.T, label string, got, want *int) {
	t.Helper()

	switch {
	case got == nil && want == nil:
		return
	case got == nil:
		t.Errorf("%s = nil, want %d", label, *want)
	case want == nil:
		t.Errorf("%s = %d, want nil", label, *got)
	case *got != *want:
		t.Errorf("%s = %d, want %d", label, *got, *want)
	}
}

func assertStrings(t *testing.T, got, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("got %v (len %d), want %v (len %d)", got, len(got), want, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d = %q, want %q", i, got[i], want[i])
		}
	}
}
