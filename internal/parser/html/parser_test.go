package html_test

import (
	"testing"

	"github.com/RikuShimoida/job-hunt-agent/internal/parser"
	htmlparser "github.com/RikuShimoida/job-hunt-agent/internal/parser/html"
)

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		wantFields parser.Fields
	}{
		{
			name: "table レイアウトから項目を抽出する",
			body: `<html><body>
				<h1>Java／AWS 基盤改善案件</h1>
				<table>
					<tr><th>企業名</th><td>架空テクノロジー株式会社</td></tr>
					<tr><th>想定単価</th><td>75〜85万円</td></tr>
					<tr><th>リモート</th><td>フルリモート</td></tr>
					<tr><th>必須スキル</th><td>Java、Spring、AWS</td></tr>
				</table>
				<a href="https://example.test/jobs/1">詳細</a>
			</body></html>`,
			wantFields: parser.Fields{
				parser.FieldTitle:          "Java／AWS 基盤改善案件",
				parser.FieldCompany:        "架空テクノロジー株式会社",
				parser.FieldRate:           "75〜85万円",
				parser.FieldRemote:         "フルリモート",
				parser.FieldRequiredSkills: "Java、Spring、AWS",
				parser.FieldURL:            "https://example.test/jobs/1",
			},
		},
		{
			name: "dl レイアウトから項目を抽出する",
			body: `<html><body>
				<h1>Rails 新規開発案件</h1>
				<dl>
					<dt>クライアント</dt><dd>架空コマース株式会社</dd>
					<dt>単価</dt><dd>5,000円/時</dd>
					<dt>働き方</dt><dd>週2日出社</dd>
				</dl>
			</body></html>`,
			wantFields: parser.Fields{
				parser.FieldTitle:   "Rails 新規開発案件",
				parser.FieldCompany: "架空コマース株式会社",
				parser.FieldRate:    "5,000円/時",
				parser.FieldRemote:  "週2日出社",
			},
		},
		{
			name: "想定タグが欠けていても取れた項目だけ返す",
			body: `<html><body>
				<table>
					<tr><th>単価</th><td>80万円</td></tr>
				</table>
			</body></html>`,
			wantFields: parser.Fields{
				parser.FieldRate: "80万円",
			},
		},
		{
			name:       "空 HTML は項目が取れない",
			body:       "",
			wantFields: parser.Fields{},
		},
		{
			name: "未知のラベルは無視する",
			body: `<html><body>
				<h1>案件</h1>
				<table>
					<tr><th>謎の項目</th><td>謎の値</td></tr>
				</table>
			</body></html>`,
			wantFields: parser.Fields{
				parser.FieldTitle: "案件",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := htmlparser.Parse(tt.body)
			if err != nil {
				t.Fatalf("Parse() returned error: %v", err)
			}
			if len(got) != len(tt.wantFields) {
				t.Fatalf("fields = %v (len %d), want %v (len %d)",
					got, len(got), tt.wantFields, len(tt.wantFields))
			}
			for k, want := range tt.wantFields {
				v, ok := got[k]
				if !ok {
					t.Errorf("field %q is missing", k)
					continue
				}
				if v != want {
					t.Errorf("field %q = %q, want %q", k, v, want)
				}
			}
		})
	}
}
