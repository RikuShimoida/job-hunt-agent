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
			name:     "区切りの両側に万が付く範囲表記でも上限を取りこぼさない",
			input:    "65万〜90万円",
			wantType: model.RateTypeMonthly,
			wantMin:  ptr(650000),
			wantMax:  ptr(900000),
		},
		{
			name:     "両側に万が付く範囲表記（チルダ区切り）",
			input:    "70万～85万円",
			wantType: model.RateTypeMonthly,
			wantMin:  ptr(700000),
			wantMax:  ptr(850000),
		},
		{
			name:     "両側に万が付く範囲表記（ハイフン区切り）",
			input:    "60万-75万円",
			wantType: model.RateTypeMonthly,
			wantMin:  ptr(600000),
			wantMax:  ptr(750000),
		},
		{
			name:     "万円単位の単一値は min と max が同じになる",
			input:    "80万円",
			wantType: model.RateTypeMonthly,
			wantMin:  ptr(800000),
			wantMax:  ptr(800000),
		},
		{
			name:     "以上表記は単一値として扱う",
			input:    "100万以上",
			wantType: model.RateTypeMonthly,
			wantMin:  ptr(1000000),
			wantMax:  ptr(1000000),
		},
		{
			name:     "小数を含む万円表記",
			input:    "72.5〜88.5万円",
			wantType: model.RateTypeMonthly,
			wantMin:  ptr(725000),
			wantMax:  ptr(885000),
		},
		{
			name:     "カンマ区切りの円表記を月額へ変換する",
			input:    "750,000円",
			wantType: model.RateTypeMonthly,
			wantMin:  ptr(750000),
			wantMax:  ptr(750000),
		},
		{
			name:     "円を伴わない通貨記号だけの表記も月額として読む",
			input:    "～￥850,000/月程度（週5日稼働換算・税別/スキル見合い）",
			wantType: model.RateTypeMonthly,
			wantMin:  ptr(850000),
			wantMax:  ptr(850000),
		},
		{
			name:     "通貨記号の範囲表記は上限まで取る",
			input:    "¥600,000～¥1,000,000/月",
			wantType: model.RateTypeMonthly,
			wantMin:  ptr(600000),
			wantMax:  ptr(1000000),
		},
		{
			name:     "上限のみの万円表記は単一値として扱う",
			input:    "～75万円(税抜)※スキル見合い",
			wantType: model.RateTypeMonthly,
			wantMin:  ptr(750000),
			wantMax:  ptr(750000),
		},
		{
			name:     "通貨記号の時給表記は hourly になる",
			input:    "￥5,000/時",
			wantType: model.RateTypeHourly,
			wantMin:  ptr(5000),
			wantMax:  ptr(5000),
		},
		{
			name:     "通貨記号と円が混在する範囲表記でも下限を取りこぼさない",
			input:    "￥600,000～1,000,000円",
			wantType: model.RateTypeMonthly,
			wantMin:  ptr(600000),
			wantMax:  ptr(1000000),
		},
		{
			name:     "但し書きの少額を上限として拾わない",
			input:    "月額 ￥850,000（交通費別途 500円）",
			wantType: model.RateTypeMonthly,
			wantMin:  ptr(850000),
			wantMax:  ptr(850000),
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
		{name: "平日週5日", input: "平日週5日", wantMin: ptr(5), wantMax: ptr(5)},
		{name: "週を伴わない5日表記", input: "5日 / フルリモート", wantMin: ptr(5), wantMax: ptr(5)},
		{name: "週を伴わない範囲表記", input: "3〜4日", wantMin: ptr(3), wantMax: ptr(4)},
		{name: "月間日数を週の稼働日数として読まない", input: "月20日稼働", wantMin: nil, wantMax: nil},
		{name: "日次の労働時間を週の稼働日数として読まない", input: "1日8時間", wantMin: nil, wantMax: nil},
		{name: "月間時間と日次時間が並んでも読まない", input: "月160時間（1日8時間×20日）", wantMin: nil, wantMax: nil},
		{name: "月間日数の後ろに続く週の稼働日数を拾う", input: "月20日稼働（週3日）", wantMin: ptr(3), wantMax: ptr(3)},
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
		{
			name:       "基本リモートは hybrid（出社日数は読み取れないので nil）",
			input:      "六本木駅 ※基本リモート（必要に応じて出社あり）",
			wantType:   model.RemoteTypeHybrid,
			wantOnsite: nil,
		},
		{name: "一部リモートは hybrid", input: "一部リモート", wantType: model.RemoteTypeHybrid, wantOnsite: nil},
		{name: "原則リモートは hybrid", input: "原則リモート", wantType: model.RemoteTypeHybrid, wantOnsite: nil},
		{name: "リモート中心は hybrid", input: "リモート中心", wantType: model.RemoteTypeHybrid, wantOnsite: nil},
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

func TestExtractSkills(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "自然文の箇条書きからスキル名を抽出する",
			input: "・Java、JavaScriptでのWebアプリケーション開発経験（5年以上目安）",
			want:  []string{"Java", "JavaScript"},
		},
		{
			name:  "スラッシュ区切りを含む自然文からも抽出する",
			input: "・PHP/Laravelでのサーバーサイドの実務経験3年以上",
			want:  []string{"PHP", "Laravel"},
		},
		{
			name: "複数行の必須スキルから出現順に抽出する",
			input: "≪必須経験・スキル≫\n" +
				"・Java、JavaScriptでのWebアプリケーション開発経験（5年以上目安）\n" +
				"・AWS環境での構築経験\n" +
				"・Terraform によるコード管理",
			want: []string{"Java", "JavaScript", "AWS", "Terraform"},
		},
		{
			name:  "別名は正規名へ寄せる",
			input: "AWS CDK を用いた IaC の経験",
			want:  []string{"AWS", "CDK"},
		},
		{
			name:  "日本語の役割名も抽出する",
			input: "バックエンドエンジニアとしてご参画いただきます",
			want:  []string{"バックエンド"},
		},
		{
			name:  "Go を Google から誤抽出しない",
			input: "Google Cloud の利用経験",
			want:  nil,
		},
		{
			name:  "単独の Go は抽出する",
			input: "Go でのバックエンド開発",
			want:  []string{"Go", "バックエンド"},
		},
		{
			name:  "辞書に無い語は抽出しない",
			input: "開発標準「TERASOLUNA」の経験",
			want:  nil,
		},
		{
			name:  "英文の next から Next.js を誤抽出しない",
			input: "next step としてご返信ください",
			want:  nil,
		},
		{
			// Next.js は JavaScript フレームワークであり、JavaScript を併せて拾うのは誤りではない。
			name:  "Next.js からは JavaScript も併せて抽出する",
			input: "Next.js でのフロントエンド開発",
			want:  []string{"Next.js", "JavaScript", "フロントエンド"},
		},
		{
			name:  "空文字は nil",
			input: "",
			want:  nil,
		},
		{
			name:  "スキルを含まない文は nil",
			input: "ご検討のほど、何卒よろしくお願い申し上げます",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := normalization.ExtractSkills(tt.input)
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
