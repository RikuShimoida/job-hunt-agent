package email_test

import (
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
		parser.FieldJobID:   "JA-000001",
		parser.FieldSummary: "要件定義、基本設計、詳細設計、実装、テスト、運用保守",
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
