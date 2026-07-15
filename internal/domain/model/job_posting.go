package model

import "time"

// RawJob はコネクタが取得した未加工の案件。
// 出所ごとの生データであり、正規化前のためスキーマは緩い。
type RawJob struct {
	SourceName string
	// メール本文 / HTML 本文など、コネクタが取得した原文。
	Body string
	// メールコネクタは "email"、HTML コネクタは "html" を入れる。
	Format string
	// メールの Message-ID や案件ページの URL など、出所を辿るための識別子。
	ExternalID string
	SourceURL  string
	Sender     string
	ReceivedAt *time.Time
}

// JobPosting は正規化済みの案件。パイプラインの共通モデル。
// 抽出できなかった項目はポインタの nil で保持し、
// 「値が 0」と「値が取れなかった」を区別する。
type JobPosting struct {
	ID          int64
	Title       string
	CompanyName string
	Summary     string
	RawText     string

	RateType RateType
	RateMin  *int
	RateMax  *int
	Currency string

	WorkDaysMin     *int
	WorkDaysMax     *int
	MonthlyHoursMin *int
	MonthlyHoursMax *int

	RemoteType RemoteType
	OnsiteDays *int
	Location   string

	StartDate    *time.Time
	EndDate      *time.Time
	ContractType string

	RequiredSkills  []string
	PreferredSkills []string
	Roles           []string

	SourceURL string
	// ApplyURL は応募（エントリー）フォームの URL。SourceURL とは別に持つ。
	//
	// SourceURL へ相乗りさせないのは、応募 URL が提携企業ごとに共通で案件ごとに
	// 一意でないため。DedupKey は SourceURL を "url:" 鍵に使うので、共通 URL が
	// 流れ込むと同一企業の複数案件が同じ鍵になり、「衝突したら既存行を更新する」
	// 仕様で先に保存した案件が上書きされて消える。
	ApplyURL    string
	PublishedAt *time.Time
	FirstSeenAt time.Time
	LastSeenAt  time.Time

	// DedupKey は重複判定の唯一のキー。SourceURL があればそれを、
	// 無ければ本文から導出した ContentHash を入れる。
	DedupKey    string
	ContentHash string

	Status           JobStatus
	Score            int
	ScoreReasons     []string
	RejectionReasons []string

	// Sources は同一案件を紹介してきた出所。複数エージェントからの
	// 紹介をまとめても、紹介元は失わずに保持する。
	Sources []JobSource
}

// HasContent は案件として最低限のコンテンツ（案件名・企業名・単価のいずれか）を
// 持つかを返す。事務連絡メールのように title も企業名も単価も取れないものは、
// 空レコードとして保存せずスキップするための判定に使う。
//
// title 単独で弾かないのは、「抽出できない項目は null で保存しパイプラインを
// 落とさない」方針と衝突するため。企業名や単価が取れているのに title の抽出だけ
// 失敗した案件を捨てると、良案件の取りこぼしになる。
//
// 単価は「読めた」ことを RateType が monthly / hourly のいずれかであることで
// 判定する。RateTypeUnknown だけでなくゼロ値（空文字）も「取れていない」に含める
// （正規化は必ず RateTypeUnknown を返すが、ゼロ値の JobPosting を誤って
// コンテンツありとみなさないため）。
func (j JobPosting) HasContent() bool {
	hasRate := j.RateType == RateTypeMonthly || j.RateType == RateTypeHourly
	return j.Title != "" || j.CompanyName != "" || hasRate
}

// JobSource は案件の紹介元。1つの JobPosting に複数ぶら下がる。
type JobSource struct {
	ID             int64
	JobID          int64
	SourceName     string
	ExternalID     string
	SourceURL      string
	EmailMessageID string
	Sender         string
	ReceivedAt     *time.Time
}
