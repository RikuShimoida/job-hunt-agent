// Package email は案件紹介メールの本文から項目を抽出する。
//
// Phase 1 の fixture 形式（ヘッダ + 「ラベル: 値」の行）だけを対象にする。
// 実エージェントのメール形式は Phase 3 で実物を確認してから対応する。
package email

import (
	"bufio"
	"strings"

	"github.com/RikuShimoida/job-hunt-agent/internal/parser"
)

// labelAliases はメール本文のラベル表記を正規キーへ寄せる。
var labelAliases = map[string]string{
	"案件名":     parser.FieldTitle,
	"案件":      parser.FieldTitle,
	"件名":      parser.FieldTitle,
	"ポジション":   parser.FieldTitle,
	"企業":      parser.FieldCompany,
	"企業名":     parser.FieldCompany,
	"クライアント":  parser.FieldCompany,
	"エージェント":  parser.FieldCompany,
	"単価":      parser.FieldRate,
	"想定単価":    parser.FieldRate,
	"月額":      parser.FieldRate,
	"報酬":      parser.FieldRate,
	"勤務地":     parser.FieldLocation,
	"就業場所":    parser.FieldLocation,
	"リモート":    parser.FieldRemote,
	"働き方":     parser.FieldRemote,
	"稼働":      parser.FieldWorkDays,
	"稼働日数":    parser.FieldWorkDays,
	"稼働時間":    parser.FieldMonthlyHours,
	"月間稼働":    parser.FieldMonthlyHours,
	"期間":      parser.FieldStartDate,
	"開始時期":    parser.FieldStartDate,
	"参画時期":    parser.FieldStartDate,
	"契約形態":    parser.FieldContractType,
	"必須スキル":   parser.FieldRequiredSkills,
	"必須":      parser.FieldRequiredSkills,
	"歓迎スキル":   parser.FieldPreferredSkills,
	"歓迎":      parser.FieldPreferredSkills,
	"役割":      parser.FieldRoles,
	"ポジション区分": parser.FieldRoles,
	"url":     parser.FieldURL,
	"URL":     parser.FieldURL,
	"詳細":      parser.FieldURL,
	"概要":      parser.FieldSummary,
}

// Header はメールのヘッダ部。
type Header struct {
	From      string
	Subject   string
	Date      string
	MessageID string
}

// Parse はメール本文からヘッダと項目を抽出する。
// ラベルが1つも取れなくてもエラーにはしない（呼び出し側が空の JobPosting を判断する）。
func Parse(body string) (Header, parser.Fields) {
	var (
		h        Header
		fields   = parser.Fields{}
		inHeader = true
	)

	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for sc.Scan() {
		line := sc.Text()

		if inHeader {
			if strings.TrimSpace(line) == "" {
				inHeader = false
				continue
			}
			key, value, ok := splitLabel(line)
			if !ok {
				continue
			}
			switch strings.ToLower(key) {
			case "from":
				h.From = value
			case "subject":
				h.Subject = value
			case "date":
				h.Date = value
			case "message-id":
				h.MessageID = value
			}
			continue
		}

		key, value, ok := splitLabel(line)
		if !ok || value == "" {
			continue
		}
		if canonical, known := labelAliases[key]; known {
			// 同じラベルが複数行に現れた場合は最初の値を採る。
			if _, exists := fields[canonical]; !exists {
				fields[canonical] = value
			}
		}
	}

	if _, ok := fields[parser.FieldTitle]; !ok && h.Subject != "" {
		fields[parser.FieldTitle] = h.Subject
	}
	return h, fields
}

// splitLabel は「ラベル: 値」「ラベル：値」を分解する。
func splitLabel(line string) (key, value string, ok bool) {
	runes := []rune(line)
	for i, r := range runes {
		if r != ':' && r != '：' {
			continue
		}
		key = strings.TrimSpace(string(runes[:i]))
		value = strings.TrimSpace(string(runes[i+1:]))
		if key == "" {
			return "", "", false
		}
		return key, value, true
	}
	return "", "", false
}
