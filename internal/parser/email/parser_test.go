package email_test

import (
	"testing"

	"github.com/RikuShimoida/job-hunt-agent/internal/parser"
	"github.com/RikuShimoida/job-hunt-agent/internal/parser/email"
)

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		wantHeader email.Header
		wantFields parser.Fields
	}{
		{
			name: "半角コロンの本文から全項目を抽出する",
			body: "From: agent@example.test\n" +
				"Subject: 新着案件のご案内\n" +
				"Date: 2026-07-01\n" +
				"Message-ID: fixture-001\n" +
				"\n" +
				"案件名: Java／AWS 基盤改善案件\n" +
				"企業: 架空テクノロジー株式会社\n" +
				"想定単価: 75〜85万円\n" +
				"勤務地: 東京\n" +
				"リモート: フルリモート\n" +
				"稼働: 週3日\n" +
				"参画時期: 2026-09-01\n" +
				"必須スキル: Java、Spring、AWS\n" +
				"URL: https://example.test/jobs/1\n",
			wantHeader: email.Header{
				From:      "agent@example.test",
				Subject:   "新着案件のご案内",
				Date:      "2026-07-01",
				MessageID: "fixture-001",
			},
			wantFields: parser.Fields{
				parser.FieldTitle:          "Java／AWS 基盤改善案件",
				parser.FieldCompany:        "架空テクノロジー株式会社",
				parser.FieldRate:           "75〜85万円",
				parser.FieldLocation:       "東京",
				parser.FieldRemote:         "フルリモート",
				parser.FieldWorkDays:       "週3日",
				parser.FieldStartDate:      "2026-09-01",
				parser.FieldRequiredSkills: "Java、Spring、AWS",
				parser.FieldURL:            "https://example.test/jobs/1",
			},
		},
		{
			name: "全角コロンとラベル別名を解釈する",
			body: "From: sales@example.test\n" +
				"Subject: フロント刷新案件\n" +
				"\n" +
				"案件名：React 刷新案件\n" +
				"クライアント：架空デジタル\n" +
				"月額：80万円\n" +
				"働き方：リモート可（週1出社）\n",
			wantHeader: email.Header{
				From:    "sales@example.test",
				Subject: "フロント刷新案件",
			},
			wantFields: parser.Fields{
				parser.FieldTitle:   "React 刷新案件",
				parser.FieldCompany: "架空デジタル",
				parser.FieldRate:    "80万円",
				parser.FieldRemote:  "リモート可（週1出社）",
			},
		},
		{
			name: "案件名が無ければ Subject を案件名に使う",
			body: "From: info@example.test\n" +
				"Subject: Go 基盤運用案件\n" +
				"\n" +
				"企業: 架空クラウド\n" +
				"リモート: フルリモート\n",
			wantHeader: email.Header{
				From:    "info@example.test",
				Subject: "Go 基盤運用案件",
			},
			wantFields: parser.Fields{
				parser.FieldTitle:   "Go 基盤運用案件",
				parser.FieldCompany: "架空クラウド",
				parser.FieldRemote:  "フルリモート",
			},
		},
		{
			name:       "空本文はヘッダも項目も空になる",
			body:       "",
			wantHeader: email.Header{},
			wantFields: parser.Fields{},
		},
		{
			name: "ラベルが1つも無い本文では項目が取れない",
			body: "From: info@example.test\n" +
				"\n" +
				"いつもお世話になっております。\n" +
				"詳細は添付をご覧ください。\n",
			wantHeader: email.Header{From: "info@example.test"},
			wantFields: parser.Fields{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotHeader, gotFields := email.Parse(tt.body)

			if gotHeader != tt.wantHeader {
				t.Errorf("header = %+v, want %+v", gotHeader, tt.wantHeader)
			}
			if len(gotFields) != len(tt.wantFields) {
				t.Fatalf("fields = %v (len %d), want %v (len %d)",
					gotFields, len(gotFields), tt.wantFields, len(tt.wantFields))
			}
			for k, want := range tt.wantFields {
				got, ok := gotFields[k]
				if !ok {
					t.Errorf("field %q is missing", k)
					continue
				}
				if got != want {
					t.Errorf("field %q = %q, want %q", k, got, want)
				}
			}
		})
	}
}
