// Package normalization は抽出済みの生文字列を共通モデルの値へ揃える。
//
// 抽出できなかった項目は nil を返す。ゼロ値を返すと「単価0円の案件」と
// 「単価が読み取れなかった案件」が区別できなくなるため。
package normalization

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
)

var (
	// 「75〜85万円」「65万〜90万円」。単一の正規表現で両方を賄わないのは、
	// 区切りの前の「万」を任意にすると「65万〜90万円」で最初の 65万 だけが
	// 拾われ、上限が捨てられるため。範囲を先に試し、単一表記へフォールバックする。
	manYenRangeRe = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*万?\s*(?:〜|～|~|-|ー)\s*(\d+(?:\.\d+)?)\s*万`)
	// 「80万円」「月額 75万」「100万以上」など
	manYenRe = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*万`)
	// 「750,000円」「5000円/時」など
	yenRe = regexp.MustCompile(`([\d,]+)\s*円`)
	// 「￥850,000」「¥600,000」。「円」を伴わない通貨記号だけの表記。
	yenSignRe = regexp.MustCompile(`[¥￥]\s*([\d,]+)`)
	// 「週3日」「週3〜4日」「週3-4」
	workDaysRe = regexp.MustCompile(`週\s*(\d)\s*(?:〜|～|~|-|ー)?\s*(\d)?\s*日?`)
	// 「5日」「3〜4日」。「週」を伴わない稼働表記。
	bareWorkDaysRe = regexp.MustCompile(`(\d)\s*(?:〜|～|~|-|ー)?\s*(\d)?\s*日`)
	// 「140〜180時間」
	hoursRe = regexp.MustCompile(`(\d{2,3})\s*(?:〜|～|~|-|ー)\s*(\d{2,3})\s*時間`)
	// 「週1出社」「月2回出社」
	onsiteDaysRe = regexp.MustCompile(`週\s*(\d)\s*(?:日)?\s*(?:程度)?\s*出社`)
)

// skillAliases は表記ゆれを正規名へ寄せる。キーは小文字化して比較する。
var skillAliases = map[string]string{
	"js":            "JavaScript",
	"javascript":    "JavaScript",
	"ts":            "TypeScript",
	"typescript":    "TypeScript",
	"aws cdk":       "CDK",
	"cdk":           "CDK",
	"golang":        "Go",
	"go":            "Go",
	"java":          "Java",
	"spring":        "Spring",
	"spring boot":   "Spring",
	"springboot":    "Spring",
	"react":         "React",
	"react.js":      "React",
	"reactjs":       "React",
	"next":          "Next.js",
	"next.js":       "Next.js",
	"nextjs":        "Next.js",
	"aws":           "AWS",
	"docker":        "Docker",
	"k8s":           "Kubernetes",
	"kubernetes":    "Kubernetes",
	"terraform":     "Terraform",
	"php":           "PHP",
	"laravel":       "Laravel",
	"ruby":          "Ruby",
	"rails":         "Ruby on Rails",
	"ruby on rails": "Ruby on Rails",
	"graphql":       "GraphQL",
	"postgres":      "PostgreSQL",
	"postgresql":    "PostgreSQL",
	"mysql":         "MySQL",
	"redis":         "Redis",
	"linux":         "Linux",
	"sre":           "SRE",
	"snowflake":     "Snowflake",
	"lambda":        "Lambda",
	"ecs":           "ECS",
	"fargate":       "Fargate",
	"s3":            "S3",
	"rds":           "RDS",
	"scrum":         "スクラム",
	"スクラム":          "スクラム",
	"バックエンド":        "バックエンド",
	"フロントエンド":       "フロントエンド",
	"インフラ":          "インフラ",
	"pm":            "PM",
	"プロジェクトマネージャー":  "PM",
	"要件定義":          "要件定義",
}

// Rate は「80万円」「5,000円/時」等を単価へ正規化する。
// 判別できない場合は RateTypeUnknown と nil を返す。
func Rate(s string) (model.RateType, *int, *int) {
	s = strings.TrimSpace(s)
	if s == "" {
		return model.RateTypeUnknown, nil, nil
	}

	hourly := strings.Contains(s, "時給") ||
		strings.Contains(s, "/時") || strings.Contains(s, "／時") ||
		strings.Contains(s, "円/h") || strings.Contains(s, "時間単価")

	if minV, maxV, ok := manYen(s); ok {
		return rateOf(hourly, minV, maxV)
	}

	if minV, maxV, ok := plainYen(s); ok {
		return rateOf(hourly, minV, maxV)
	}

	return model.RateTypeUnknown, nil, nil
}

func rateOf(hourly bool, minV, maxV int) (model.RateType, *int, *int) {
	if hourly {
		return model.RateTypeHourly, &minV, &maxV
	}
	return model.RateTypeMonthly, &minV, &maxV
}

// plainYen は円表記の単価を範囲として返す。
//
// 「円」を必須にしないのは、実エージェントのメールに「～￥850,000/月程度」のような
// 通貨記号だけの表記があるため。「円」を要求すると単価が RateTypeUnknown になり、
// minimum_rate による除外も target_rate による加点も一切効かなくなる。
func plainYen(s string) (minV, maxV int, ok bool) {
	ms := yenRe.FindAllStringSubmatch(s, -1)
	if len(ms) == 0 {
		ms = yenSignRe.FindAllStringSubmatch(s, -1)
	}
	if len(ms) == 0 {
		return 0, 0, false
	}

	values := make([]int, 0, len(ms))
	for _, m := range ms {
		v, err := strconv.Atoi(strings.ReplaceAll(m[1], ",", ""))
		if err != nil {
			continue
		}
		values = append(values, v)
	}
	if len(values) == 0 {
		return 0, 0, false
	}
	return values[0], values[len(values)-1], true
}

// manYen は「万」表記の単価を範囲として返す。範囲表記を先に試すのは、
// 単一表記の正規表現は「65万〜90万円」の先頭 65万 にも一致してしまい、
// 先に評価すると上限を取りこぼすため。
func manYen(s string) (minV, maxV int, ok bool) {
	if m := manYenRangeRe.FindStringSubmatch(s); m != nil {
		lo, loOK := parseManYen(m[1])
		hi, hiOK := parseManYen(m[2])
		if loOK && hiOK {
			return lo, hi, true
		}
	}
	if m := manYenRe.FindStringSubmatch(s); m != nil {
		if v, valid := parseManYen(m[1]); valid {
			return v, v, true
		}
	}
	return 0, 0, false
}

func parseManYen(s string) (int, bool) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return int(f * 10000), true
}

// WorkDays は「週3日」「週3〜4日」「5日」を稼働日数の範囲へ正規化する。
func WorkDays(s string) (*int, *int) {
	// 「週」ありを先に試すのは、「平日週5日」のような表記に対して
	// 「週」なしのパターンを先に当てると意図しない桁を拾いうるため。
	if minV, maxV, ok := matchWorkDays(workDaysRe, s); ok {
		return &minV, &maxV
	}
	// 「稼働：5日 / フルリモート」のように「週」を伴わない表記があるため、
	// 「週」を必須にすると稼働日数が読めない。
	if minV, maxV, ok := matchWorkDays(bareWorkDaysRe, s); ok {
		return &minV, &maxV
	}
	return nil, nil
}

func matchWorkDays(re *regexp.Regexp, s string) (minV, maxV int, ok bool) {
	m := re.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, false
	}
	minV, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, 0, false
	}
	maxV = minV
	if m[2] != "" {
		v, err := strconv.Atoi(m[2])
		if err != nil {
			return 0, 0, false
		}
		maxV = v
	}
	// 「月20日稼働」のような月間日数を週の稼働日数として読まないための範囲チェック。
	// 1桁ずつ拾う正規表現のため、"20日" は min=2 / max=0 という不整合な組で返る。
	if minV < 1 || maxV < minV || maxV > 7 {
		return 0, 0, false
	}
	return minV, maxV, true
}

// MonthlyHours は「140〜180時間」を月間稼働時間の範囲へ正規化する。
func MonthlyHours(s string) (*int, *int) {
	m := hoursRe.FindStringSubmatch(s)
	if m == nil {
		return nil, nil
	}
	minV, err := strconv.Atoi(m[1])
	if err != nil {
		return nil, nil
	}
	maxV, err := strconv.Atoi(m[2])
	if err != nil {
		return nil, nil
	}
	return &minV, &maxV
}

// Remote はリモート条件を分類し、出社日数が読み取れれば併せて返す。
func Remote(s string) (model.RemoteType, *int) {
	s = strings.TrimSpace(s)
	if s == "" {
		return model.RemoteTypeUnknown, nil
	}

	if m := onsiteDaysRe.FindStringSubmatch(s); m != nil {
		if days, err := strconv.Atoi(m[1]); err == nil {
			if days == 0 {
				return model.RemoteTypeFullRemote, &days
			}
			return model.RemoteTypeHybrid, &days
		}
	}

	switch {
	case strings.Contains(s, "フルリモート"), strings.Contains(s, "完全リモート"),
		strings.Contains(s, "フルリモ"):
		return model.RemoteTypeFullRemote, nil
	case strings.Contains(s, "常駐"), strings.Contains(s, "出社必須"),
		strings.Contains(s, "フル出社"):
		return model.RemoteTypeOnsite, nil
	// 「基本リモート（必要に応じて出社あり）」を unknown のままにすると、
	// scorer の reject は unknown を除外しないため、remote_required（出社0日のみ許容）の
	// 利用者へ出社を伴う案件が通知される。出社日数が読めない場合は nil のままとし、
	// ハイブリッドとして扱う。
	case strings.Contains(s, "ハイブリッド"), strings.Contains(s, "一部リモート"),
		strings.Contains(s, "リモート可"), strings.Contains(s, "リモート併用"),
		strings.Contains(s, "基本リモート"), strings.Contains(s, "原則リモート"),
		strings.Contains(s, "リモート中心"):
		return model.RemoteTypeHybrid, nil
	default:
		return model.RemoteTypeUnknown, nil
	}
}

// Skills はスキル表記の揺れを正規名へ寄せ、重複を除いて返す。
// 別名テーブルに無い語はトリムしてそのまま残す（勝手に捨てない）。
func Skills(items []string) []string {
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))

	for _, raw := range items {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if canonical, ok := skillAliases[strings.ToLower(s)]; ok {
			s = canonical
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// skillMatcher は skillAliases の1エントリを本文照合に使えるようにしたもの。
type skillMatcher struct {
	re        *regexp.Regexp
	substring string
	canonical string
}

// skillMatchers は skillAliases を自然文からの抽出に使えるようにしたもの。
//
// ASCII の別名だけ単語境界つきの正規表現にするのは、単純な部分一致にすると
// 「go」が「Google」に、「ts」が「sports」に当たって誤抽出されるため。
// 日本語の別名は単語境界の概念がなく、部分一致で問題ない。
var skillMatchers = buildSkillMatchers()

func buildSkillMatchers() []skillMatcher {
	out := make([]skillMatcher, 0, len(skillAliases))
	for alias, canonical := range skillAliases {
		if isASCII(alias) {
			out = append(out, skillMatcher{
				re:        regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(alias) + `\b`),
				canonical: canonical,
			})
			continue
		}
		out = append(out, skillMatcher{substring: alias, canonical: canonical})
	}
	return out
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// index は本文中で最初に現れる位置を返す。見つからなければ -1。
func (m skillMatcher) index(text string) int {
	if m.re != nil {
		loc := m.re.FindStringIndex(text)
		if loc == nil {
			return -1
		}
		return loc[0]
	}
	return strings.Index(text, m.substring)
}

// ExtractSkills は自然文からスキル名を抽出し、本文での出現順に返す。
//
// SplitList で分解しないのは、実エージェントのメールがスキルを区切り文字で列挙せず
// 「・Java、JavaScriptでのWebアプリケーション開発経験（5年以上目安）」のような文章で
// 書くため。区切り文字だけで割ると文章がそのまま1スキルとして残り、matching の
// intersect は完全一致で照合するので得意スキルと一致しない。
func ExtractSkills(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}

	firstAt := make(map[string]int, len(skillMatchers))
	for _, m := range skillMatchers {
		at := m.index(text)
		if at < 0 {
			continue
		}
		// 「js」と「javascript」のように複数の別名が同じ正規名へ寄るため、
		// 最も早い出現位置を採って並び順を安定させる。
		if cur, ok := firstAt[m.canonical]; !ok || at < cur {
			firstAt[m.canonical] = at
		}
	}
	if len(firstAt) == 0 {
		return nil
	}

	out := make([]string, 0, len(firstAt))
	for canonical := range firstAt {
		out = append(out, canonical)
	}
	// map の反復順は不定であり、そのまま返すと呼び出しごとに順序が変わる。
	// required_skills は MaterialChanges の比較対象であり、順序が揺れると
	// 内容が変わっていない案件が「重要変更あり」と誤判定されて再通知される。
	sort.Slice(out, func(i, j int) bool {
		if firstAt[out[i]] != firstAt[out[j]] {
			return firstAt[out[i]] < firstAt[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}

// SplitList は「Java、Spring / AWS,Docker」のような区切り文字混在の列挙を分解する。
func SplitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	fields := strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case '、', ',', '，', '/', '・', '|', '\n':
			return true
		default:
			return false
		}
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if t := strings.TrimSpace(f); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
