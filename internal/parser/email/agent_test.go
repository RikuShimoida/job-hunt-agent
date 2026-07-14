package email_test

import (
	"strings"
	"testing"

	"github.com/RikuShimoida/job-hunt-agent/internal/parser"
	"github.com/RikuShimoida/job-hunt-agent/internal/parser/email"
)

// crowdTechBody は実際のクラウドワークス テック提携企業案件メールと同じ書式。
// 企業名・案件 ID・URL は架空値へ置き換えている。
const crowdTechBody = `架空 太郎様

お世話になっております。
クラウドワークス テック事務局 アライアンスグループでございます。

==========｜ご紹介案件｜==========
■案件名
【Java/週5日/フルリモート】衛星地上局の開発業務案件

■働き方
・金額：～￥850,000/月程度（週5日稼働換算・税別/スキル見合い）
・稼働：5日 / フルリモート

■案件ID：JA-000001

■業務概要
要件定義、基本設計、詳細設計、実装、テスト、運用保守

≪必須経験・スキル≫
・Java、JavaScriptでのWebアプリケーション開発経験（5年以上目安）
・基本設計～自走ができる方

≪尚可経験・スキル≫
・要件定義またはアーキテクチャ選定などいずれかの上流工程経験
・Dockerの実務経験

■■■■エントリー方法■■■■
1.以下URLからエントリーください。
https://example-form.test/entry

==============配信停止に関するご案内==============
配信停止をご希望の場合は以下のフォームからお手続きください。
https://docs.google.com/forms/d/e/1FAIpQLSfake/viewform?usp=pp_url&entry.547304507=JA-000001
`

// fosterNetBody は実際のフォスターネット案件紹介メールと同じ書式。
const fosterNetBody = `架空 太郎様

こんにちは。フォスターネット フリーランス担当です。

ーーーーーーーーーーーーーーーーー
■案件名：【PHP】 架空ECサービス企業におけるサーバーサイドエンジニア
■案件掲載URL：https://example-agent.test/projects/detail/J00001

■期間：8月～
■稼動日数：平日週5日
■場所：六本木駅 ※基本リモート（必要に応じて出社あり）
■金額：～75万円(税抜)※スキル見合い

■概要：
架空ECサービス企業の案件にサーバーサイドエンジニアとしてご参画いただきます。

■求めるスキル：
＜必須＞
・PHP/Laravelでのサーバーサイドの実務経験3年以上
・PHPUnitに代表されるユニットテストツールの実務経験

＜尚可＞
・Dockerの実務経験
・CI/CDツール（CircleCI、Jenkins等）の実務経験

■基本時間：10:00～19:00
ーーーーーーーーーーーーーーーーー
`

func TestExtractCrowdTech(t *testing.T) {
	t.Parallel()

	fields, ok := email.Extract("alliance-crowdtech@crowdworks.co.jp", crowdTechBody)
	if !ok {
		t.Fatal("Extract() ok = false, want true")
	}

	want := map[string]string{
		// 案件名はラベルの「次の行」にある。
		parser.FieldTitle: "【Java/週5日/フルリモート】衛星地上局の開発業務案件",
		// 「円」も「万」も無い通貨記号表記。
		parser.FieldRate: "～￥850,000/月程度（週5日稼働換算・税別/スキル見合い）",
		// 稼働日数とリモート条件が同じ行に同居する。
		parser.FieldWorkDays: "5日 / フルリモート",
		parser.FieldRemote:   "5日 / フルリモート",
		// 案件詳細 URL を持たないソースの唯一の一意キー。
		parser.FieldJobID: "JA-000001",
		// 応募導線は「エントリー方法」セクション配下の URL。案件詳細 URL は持たない。
		parser.FieldApplyURL: "https://example-form.test/entry",
		parser.FieldSummary:  "要件定義、基本設計、詳細設計、実装、テスト、運用保守",
		// 自然文の箇条書きから辞書で抽出する。
		parser.FieldRequiredSkills:  "Java、JavaScript",
		parser.FieldPreferredSkills: "要件定義、Docker",
	}

	for key, wantValue := range want {
		if got := fields[key]; got != wantValue {
			t.Errorf("fields[%q] = %q, want %q", key, got, wantValue)
		}
	}
}

func TestExtractFosterNet(t *testing.T) {
	t.Parallel()

	fields, ok := email.Extract("careers.desk.haishin@foster-net.co.jp", fosterNetBody)
	if !ok {
		t.Fatal("Extract() ok = false, want true")
	}

	want := map[string]string{
		parser.FieldTitle: "【PHP】 架空ECサービス企業におけるサーバーサイドエンジニア",
		parser.FieldURL:   "https://example-agent.test/projects/detail/J00001",
		// 「稼動」（動）の異表記を拾う。
		parser.FieldWorkDays: "平日週5日",
		// 勤務地とリモート条件が同居する。「基本リモート」は hybrid に分類される。
		parser.FieldLocation:  "六本木駅 ※基本リモート（必要に応じて出社あり）",
		parser.FieldRemote:    "六本木駅 ※基本リモート（必要に応じて出社あり）",
		parser.FieldRate:      "～75万円(税抜)※スキル見合い",
		parser.FieldStartDate: "8月～",
		// 「■概要：」だけの行で、本文は次行から始まる。
		parser.FieldSummary:         "架空ECサービス企業の案件にサーバーサイドエンジニアとしてご参画いただきます。",
		parser.FieldRequiredSkills:  "PHP、Laravel",
		parser.FieldPreferredSkills: "Docker",
	}

	for key, wantValue := range want {
		if got := fields[key]; got != wantValue {
			t.Errorf("fields[%q] = %q, want %q", key, got, wantValue)
		}
	}
}

// TestExtractCrowdTechDoesNotUseUnsubscribeURL は、配信停止セクションの URL を
// 応募 URL として拾わないことを固定する（Issue #21 の回帰テスト）。
//
// 配信停止フォームの URL は案件 ID をクエリに含む（&entry.547304507=JA-000001）ため
// 応募導線に見えるが、実体は配信停止・問い合わせ用。これを「応募：」として通知すると、
// 利用者が応募したつもりで配信停止フォームを開くことになる。
func TestExtractCrowdTechDoesNotUseUnsubscribeURL(t *testing.T) {
	t.Parallel()

	fields, ok := email.Extract("alliance-crowdtech@crowdworks.co.jp", crowdTechBody)
	if !ok {
		t.Fatal("Extract() ok = false, want true")
	}

	if got := fields[parser.FieldApplyURL]; !strings.HasPrefix(got, "https://example-form.test/") {
		t.Errorf("apply_url = %q, want エントリー方法セクションの URL", got)
	}
	if got := fields[parser.FieldApplyURL]; strings.Contains(got, "docs.google.com") {
		t.Errorf("apply_url に配信停止フォームの URL を拾っている: %q", got)
	}
	// 応募 URL は案件ごとに一意でないため、重複判定に使う source_url へ入れてはならない。
	if got := fields[parser.FieldURL]; got != "" {
		t.Errorf("url = %q, want 空（応募 URL を source_url へ流し込まない）", got)
	}
}

// TestExtractCrowdTechApplyURL は応募 URL の抽出範囲を固定する。
func TestExtractCrowdTechApplyURL(t *testing.T) {
	t.Parallel()

	// 案件名だけは必須（Extract が ok=false を返さないようにするため）。
	const header = "■案件名\n【Java】架空案件\n\n"

	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "エントリー方法セクション配下の URL を採る",
			body: header + "■■■■エントリー方法■■■■\n1.以下URLからエントリーください。\nhttps://share.hsforms.test/abc123\n",
			want: "https://share.hsforms.test/abc123",
		},
		{
			name: "提携企業ごとに URL が違っても採れる",
			body: header + "■■■■エントリー方法■■■■\nhttps://forms.gle.test/xyz789\n",
			want: "https://forms.gle.test/xyz789",
		},
		{
			name: "文中に埋まった URL も採る",
			body: header + "■■■■エントリー方法■■■■\n以下より応募 https://share.hsforms.test/inline よろしくお願いします\n",
			want: "https://share.hsforms.test/inline",
		},
		{
			name: "URL より前の ※注記行では打ち切らない",
			body: header + "■■■■エントリー方法■■■■\n※担当より追ってご連絡いたします。\nhttps://share.hsforms.test/afternote\n",
			want: "https://share.hsforms.test/afternote",
		},
		{
			name: "エントリー方法より前にある URL は採らない",
			body: header + "■お知らせ\nhttps://example.test/news\n\n■■■■エントリー方法■■■■\nhttps://share.hsforms.test/entry\n",
			want: "https://share.hsforms.test/entry",
		},
		{
			name: "配信停止セクションの URL は採らない",
			body: header + "■■■■エントリー方法■■■■\n1.以下URLからエントリーください。\nhttps://share.hsforms.test/entry\n\n==============配信停止に関するご案内==============\nhttps://docs.google.com/forms/d/e/1FAIpQLSfake/viewform?usp=pp_url&entry.547304507=JA-000001\n",
			want: "https://share.hsforms.test/entry",
		},
		{
			name: "エントリー方法セクションに URL が無ければ空（後続セクションへはみ出さない）",
			body: header + "■■■■エントリー方法■■■■\n担当者へ返信してください。\n\n==============配信停止に関するご案内==============\nhttps://docs.google.com/forms/d/e/1FAIpQLSfake/viewform\n",
			want: "",
		},
		{
			name: "エントリー方法セクションが無ければ空",
			body: header + "■案件ID：JA-000002\n",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fields, ok := email.Extract("alliance-crowdtech@crowdworks.co.jp", tt.body)
			if !ok {
				t.Fatal("Extract() ok = false, want true")
			}
			if got := fields[parser.FieldApplyURL]; got != tt.want {
				t.Errorf("apply_url = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractUnknownSender(t *testing.T) {
	t.Parallel()

	if _, ok := email.Extract("noreply@unknown-agent.test", crowdTechBody); ok {
		t.Error("Extract() ok = true, want false（未対応の送信元は fixture 形式へフォールバックする）")
	}
}

// TestExtractDoesNotLeakEntrySectionIntoSkills は、スキルのセクションが
// 次の見出し（■■■■エントリー方法■■■■）で確実に切れることを固定する。
// 切れないと本文の後半（署名・注意事項）まで飲み込み、無関係な語を拾う。
func TestExtractCrowdTechStopsAtNextHeading(t *testing.T) {
	t.Parallel()

	fields, ok := email.Extract("alliance-crowdtech@crowdworks.co.jp", crowdTechBody)
	if !ok {
		t.Fatal("Extract() ok = false, want true")
	}

	// エントリー方法の節にある「1.以下URLから…」を尚可スキルへ含めていないこと。
	if got := fields[parser.FieldPreferredSkills]; got != "要件定義、Docker" {
		t.Errorf("preferred_skills = %q, want %q", got, "要件定義、Docker")
	}
}
