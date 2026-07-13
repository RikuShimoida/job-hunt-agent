// Package html は案件詳細ページの HTML から項目を抽出する。
//
// regexp ではなく golang.org/x/net/html のトークナイザを使うのは、
// 属性順や空白の変化で壊れないようにするため（Phase 4 の実サイト対応で効いてくる）。
package html

import (
	"fmt"
	"strings"

	xhtml "golang.org/x/net/html"

	"github.com/RikuShimoida/job-hunt-agent/internal/parser"
)

// labelAliases は詳細ページの見出し表記を正規キーへ寄せる。
var labelAliases = map[string]string{
	"案件名":    parser.FieldTitle,
	"企業":     parser.FieldCompany,
	"企業名":    parser.FieldCompany,
	"クライアント": parser.FieldCompany,
	"単価":     parser.FieldRate,
	"想定単価":   parser.FieldRate,
	"月額":     parser.FieldRate,
	"勤務地":    parser.FieldLocation,
	"リモート":   parser.FieldRemote,
	"働き方":    parser.FieldRemote,
	"稼働":     parser.FieldWorkDays,
	"稼働日数":   parser.FieldWorkDays,
	"稼働時間":   parser.FieldMonthlyHours,
	"開始時期":   parser.FieldStartDate,
	"参画時期":   parser.FieldStartDate,
	"契約形態":   parser.FieldContractType,
	"必須スキル":  parser.FieldRequiredSkills,
	"歓迎スキル":  parser.FieldPreferredSkills,
	"役割":     parser.FieldRoles,
	"概要":     parser.FieldSummary,
}

// Parse は HTML から案件項目を抽出する。
// <h1> を案件名、<table> の行（th/td）と <dl>（dt/dd）を条件項目として読む。
func Parse(body string) (parser.Fields, error) {
	doc, err := xhtml.Parse(strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to parse html: %w", err)
	}

	fields := parser.Fields{}
	walk(doc, fields)
	return fields, nil
}

func walk(n *xhtml.Node, fields parser.Fields) {
	if n.Type == xhtml.ElementNode {
		switch n.Data {
		case "h1":
			if _, exists := fields[parser.FieldTitle]; !exists {
				if t := text(n); t != "" {
					fields[parser.FieldTitle] = t
				}
			}
		case "tr":
			collectPair(n, fields, "th", "td")
		case "dl":
			collectDefinitionList(n, fields)
		case "a":
			if _, exists := fields[parser.FieldURL]; !exists {
				if href, ok := attr(n, "href"); ok && strings.HasPrefix(href, "http") {
					fields[parser.FieldURL] = href
				}
			}
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, fields)
	}
}

// collectPair は <tr><th>ラベル</th><td>値</td></tr> を1組として読む。
func collectPair(tr *xhtml.Node, fields parser.Fields, labelTag, valueTag string) {
	var label, value string
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != xhtml.ElementNode {
			continue
		}
		switch c.Data {
		case labelTag:
			label = text(c)
		case valueTag:
			value = text(c)
		}
	}
	assign(fields, label, value)
}

// collectDefinitionList は <dl><dt>ラベル</dt><dd>値</dd></dl> を読む。
func collectDefinitionList(dl *xhtml.Node, fields parser.Fields) {
	var label string
	for c := dl.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != xhtml.ElementNode {
			continue
		}
		switch c.Data {
		case "dt":
			label = text(c)
		case "dd":
			assign(fields, label, text(c))
			label = ""
		}
	}
}

func assign(fields parser.Fields, label, value string) {
	label = strings.TrimSpace(label)
	value = strings.TrimSpace(value)
	if label == "" || value == "" {
		return
	}
	canonical, known := labelAliases[label]
	if !known {
		return
	}
	if _, exists := fields[canonical]; exists {
		return
	}
	fields[canonical] = value
}

// text はノード配下のテキストを連結する。
func text(n *xhtml.Node) string {
	var b strings.Builder
	var rec func(*xhtml.Node)
	rec = func(node *xhtml.Node) {
		if node.Type == xhtml.TextNode {
			b.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			rec(c)
		}
	}
	rec(n)
	return strings.TrimSpace(strings.Join(strings.Fields(b.String()), " "))
}

func attr(n *xhtml.Node, name string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val, true
		}
	}
	return "", false
}
