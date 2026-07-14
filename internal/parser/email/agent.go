package email

import (
	"strings"

	"github.com/RikuShimoida/job-hunt-agent/internal/normalization"
	"github.com/RikuShimoida/job-hunt-agent/internal/parser"
)

// 対応する送信元のドメイン。ここに無い送信元は fixture 形式として解釈する。
const (
	domainCrowdTech = "crowdworks.co.jp"
	domainFosterNet = "foster-net.co.jp"
)

// Extract は送信元に応じた抽出を行う。対応していない送信元なら ok=false を返す。
//
// 送信元で分岐するのは、実エージェントのメール書式が互いに異なり、共通の
// 「ラベル: 値」規則へ寄せられないため。クラウドテックはラベルの次の行が値で、
// フォスターネットは同じ行に値が続く。
//
// **案件名すら取れなかった場合も ok=false を返す**（送信元は一致していても）。
// エージェントがメールの書式を変えたとき、空の Fields をそのまま通すと
// 中身のない JobPosting が無音で積み上がり、通知が止まったことにしか気づけない。
// fixture 形式の Parse へフォールバックさせ、それでも取れなければ件名が題名として残る。
func Extract(sender, body string) (parser.Fields, bool) {
	var fields parser.Fields

	switch {
	case strings.Contains(sender, domainCrowdTech):
		fields = extractCrowdTech(body)
	case strings.Contains(sender, domainFosterNet):
		fields = extractFosterNet(body)
	default:
		return nil, false
	}

	if fields[parser.FieldTitle] == "" {
		return nil, false
	}
	return fields, true
}

// extractCrowdTech はクラウドワークス テックの提携企業案件メールを解釈する。
//
//	■案件名
//	【Java/週5日/フルリモート】衛星地上局の開発業務案件
//	■働き方
//	・金額：～￥850,000/月程度
//	・稼働：5日 / フルリモート
//	■案件ID：JA-086984
func extractCrowdTech(body string) parser.Fields {
	f := parser.Fields{}
	lines := splitLines(body)

	for i, line := range lines {
		head := stripMarkers(line)

		if key, value, ok := splitLabel(head); ok && value != "" {
			switch key {
			case "案件ID":
				setOnce(f, parser.FieldJobID, value)
			case "金額":
				setOnce(f, parser.FieldRate, value)
			case "稼働":
				// 「5日 / フルリモート」の1行に稼働日数とリモート条件が同居する。
				setOnce(f, parser.FieldWorkDays, value)
				setOnce(f, parser.FieldRemote, value)
			}
			continue
		}

		// 値がラベルの次行に来る項目。
		switch head {
		case "案件名":
			if v, ok := nextNonEmpty(lines, i); ok {
				setOnce(f, parser.FieldTitle, v)
			}
		case "業務概要":
			if v, ok := nextNonEmpty(lines, i); ok {
				setOnce(f, parser.FieldSummary, v)
			}
		}
	}

	setOnce(f, parser.FieldApplyURL, applyURL(lines))
	setSkills(f, parser.FieldRequiredSkills, section(lines, "≪必須経験・スキル≫"))
	setSkills(f, parser.FieldPreferredSkills, section(lines, "≪尚可経験・スキル≫"))
	setRoles(f)

	return f
}

// extractFosterNet はフォスターネットの案件紹介メールを解釈する。
//
//	■案件名：【PHP】…
//	■案件掲載URL：https://freelance.fosternet.jp/projects/detail/J40748
//	■稼動日数：平日週5日
//	■場所：六本木駅 ※基本リモート（必要に応じて出社あり）
//	■金額：～75万円(税抜)
func extractFosterNet(body string) parser.Fields {
	f := parser.Fields{}
	lines := splitLines(body)

	for i, line := range lines {
		head := stripMarkers(line)

		key, value, ok := splitLabel(head)
		if !ok {
			continue
		}

		switch key {
		case "案件名":
			setOnce(f, parser.FieldTitle, value)
		case "案件掲載URL":
			setOnce(f, parser.FieldURL, value)
		case "期間":
			setOnce(f, parser.FieldStartDate, value)
		// 「稼動」は「稼働」の異表記。実メールがこの字を使う。
		case "稼動日数", "稼働日数":
			setOnce(f, parser.FieldWorkDays, value)
		case "場所":
			// 「六本木駅 ※基本リモート（必要に応じて出社あり）」に勤務地と
			// リモート条件が同居する。
			setOnce(f, parser.FieldLocation, value)
			setOnce(f, parser.FieldRemote, value)
		case "金額":
			setOnce(f, parser.FieldRate, value)
		case "概要":
			// 「■概要：」だけの行で、本文は次行から始まる。
			if value == "" {
				if v, ok := nextNonEmpty(lines, i); ok {
					setOnce(f, parser.FieldSummary, v)
				}
				continue
			}
			setOnce(f, parser.FieldSummary, value)
		}
	}

	setSkills(f, parser.FieldRequiredSkills, section(lines, "＜必須＞"))
	setSkills(f, parser.FieldPreferredSkills, section(lines, "＜尚可＞"))
	setRoles(f)

	return f
}

// entryHeading はエントリー方法のセクション見出し。実メールでは
// 「■■■■エントリー方法■■■■」のように記号で挟まれるため、部分一致で探す。
const entryHeading = "エントリー方法"

// applyURL はエントリー方法セクション配下に最初に現れる https:// の URL を返す。
//
// 本文全体から最初の https:// を拾わないのは、末尾の「配信停止に関するご案内」に
// 案件 ID をクエリへ含む Google フォームの URL（docs.google.com/forms/…&entry.NNN=<案件ID>）
// があるため。案件 ID が入っているぶん応募導線に見えるが実体は配信停止・問い合わせ用で、
// これを拾うと通知の「応募：」が配信停止フォームを指す（リンクが無い現状より悪い）。
// 探索をエントリー方法セクションの内側へ閉じることで、この取り違えを構造的に防ぐ。
func applyURL(lines []string) string {
	start := -1
	for i, l := range lines {
		if strings.Contains(l, entryHeading) {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return ""
	}

	for _, l := range lines[start:] {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if isHeading(t) {
			return ""
		}
		if u := firstHTTPS(t); u != "" {
			return u
		}
	}
	return ""
}

// firstHTTPS は行に含まれる最初の https:// URL を返す。
// URL が単独行ではなく文中に埋まる書式（「1.以下URLから → https://…」）にも耐える。
func firstHTTPS(s string) string {
	i := strings.Index(s, "https://")
	if i < 0 {
		return ""
	}
	u := s[i:]
	if j := strings.IndexAny(u, " \t　"); j >= 0 {
		u = u[:j]
	}
	return u
}

// setSkills は自然文のスキル欄から辞書ベースでスキル名を抽出して詰める。
//
// SplitList に任せないのは、実メールのスキルが「・Java、JavaScriptでの
// Webアプリケーション開発経験（5年以上目安）」のような文章であり、区切り文字で
// 割ると文がそのまま1スキルとして残って matching の完全一致に掛からないため。
func setSkills(f parser.Fields, key, text string) {
	skills := normalization.ExtractSkills(text)
	if len(skills) == 0 {
		return
	}
	f[key] = strings.Join(skills, "、")
}

// setRoles は案件名と概要から役割を拾う。
//
// 役割の専用欄を持つエージェントが無いため、案件名・概要から辞書で拾うしかない。
// 辞書はスキルと共通のため Java のような語も混ざるが、matching は
// profile.desired_roles との交差しか見ないので加点には影響しない。
func setRoles(f parser.Fields) {
	text := f[parser.FieldTitle] + "\n" + f[parser.FieldSummary]
	roles := normalization.ExtractSkills(text)
	if len(roles) == 0 {
		return
	}
	f[parser.FieldRoles] = strings.Join(roles, "、")
}

func splitLines(body string) []string {
	return strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
}

// stripMarkers は行頭の装飾記号を落とす。実メールのラベルは「■案件名」「・金額」
// のように記号が前置され、そのままでは splitLabel のキーに記号が残る。
func stripMarkers(line string) string {
	return strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "■・＊*●◆-"))
}

func setOnce(f parser.Fields, key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if _, exists := f[key]; !exists {
		f[key] = value
	}
}

// nextNonEmpty は i の次にある最初の非空行を返す。
func nextNonEmpty(lines []string, i int) (string, bool) {
	for _, l := range lines[i+1:] {
		if t := strings.TrimSpace(l); t != "" {
			return t, true
		}
	}
	return "", false
}

// section は見出し行の次から、次の見出しが現れるまでの行を連結する。
func section(lines []string, heading string) string {
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == heading {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return ""
	}

	var b strings.Builder
	for _, l := range lines[start:] {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if isHeading(t) {
			break
		}
		b.WriteString(t)
		b.WriteString("\n")
	}
	return b.String()
}

// isHeading はセクションの切れ目を判定する。
func isHeading(s string) bool {
	for _, prefix := range []string{"■", "≪", "＜", "【", "=", "―", "ー", "※"} {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}
