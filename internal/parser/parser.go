// Package parser はソース別に抽出したラベル項目を共通の JobPosting へ組み立てる。
//
// 抽出（メール / HTML）とモデル組み立てを分けているのは、ソースが増えても
// 正規化・重複キー生成のロジックを1箇所に保つため。
package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
	"github.com/RikuShimoida/job-hunt-agent/internal/normalization"
)

// 抽出結果の正規キー。ソース別パーサーはラベルをこれらへ寄せる。
const (
	FieldTitle           = "title"
	FieldCompany         = "company"
	FieldRate            = "rate"
	FieldLocation        = "location"
	FieldRemote          = "remote"
	FieldWorkDays        = "work_days"
	FieldMonthlyHours    = "monthly_hours"
	FieldStartDate       = "start_date"
	FieldContractType    = "contract_type"
	FieldRequiredSkills  = "required_skills"
	FieldPreferredSkills = "preferred_skills"
	FieldRoles           = "roles"
	FieldURL             = "url"
	// FieldApplyURL は応募（エントリー）フォームの URL。FieldURL とは別に持つ。
	// 提携企業ごとに共通で案件ごとに一意でないため、DedupKey の入力にはしない。
	FieldApplyURL = "apply_url"
	FieldSummary  = "summary"
	// FieldJobID はソースが案件へ振る一意の ID（クラウドテックの「JA-086984」など）。
	FieldJobID = "job_id"
)

// Fields はソース別パーサーが抽出した生の項目。値は未加工の文字列。
type Fields map[string]string

// Build は抽出項目を正規化して JobPosting を組み立てる。
// 抽出できなかった項目は nil / ゼロ値のままにし、エラーにはしない
// （1件の欠損でパイプライン全体を落とさないため）。
func Build(raw model.RawJob, f Fields, now time.Time) model.JobPosting {
	job := model.JobPosting{
		Title:        strings.TrimSpace(f[FieldTitle]),
		CompanyName:  strings.TrimSpace(f[FieldCompany]),
		Summary:      strings.TrimSpace(f[FieldSummary]),
		RawText:      raw.Body,
		Currency:     "JPY",
		Location:     strings.TrimSpace(f[FieldLocation]),
		ContractType: strings.TrimSpace(f[FieldContractType]),
		FirstSeenAt:  now,
		LastSeenAt:   now,
		PublishedAt:  raw.ReceivedAt,
		Status:       model.JobStatusNew,
	}

	job.RateType, job.RateMin, job.RateMax = normalization.Rate(f[FieldRate])
	job.WorkDaysMin, job.WorkDaysMax = normalization.WorkDays(f[FieldWorkDays])
	job.MonthlyHoursMin, job.MonthlyHoursMax = normalization.MonthlyHours(f[FieldMonthlyHours])
	job.RemoteType, job.OnsiteDays = normalization.Remote(f[FieldRemote])
	job.StartDate = parseDate(f[FieldStartDate])

	job.RequiredSkills = normalization.Skills(normalization.SplitList(f[FieldRequiredSkills]))
	job.PreferredSkills = normalization.Skills(normalization.SplitList(f[FieldPreferredSkills]))
	job.Roles = normalization.Skills(normalization.SplitList(f[FieldRoles]))

	job.SourceURL = strings.TrimSpace(f[FieldURL])
	if job.SourceURL == "" {
		job.SourceURL = raw.SourceURL
	}
	job.ApplyURL = strings.TrimSpace(f[FieldApplyURL])

	job.ContentHash = ContentHash(job.Title, job.CompanyName, raw.Body)
	// ApplyURL を渡さないのは、応募 URL が案件ごとに一意でないため。
	// 重複判定へ流れ込むと、同一企業の別案件が同じ鍵になって上書きされて消える。
	job.DedupKey = DedupKey(raw.SourceName, f[FieldJobID], job.SourceURL, job.ContentHash)

	job.Sources = []model.JobSource{{
		SourceName:     raw.SourceName,
		ExternalID:     raw.ExternalID,
		SourceURL:      job.SourceURL,
		EmailMessageID: emailMessageID(raw),
		Sender:         raw.Sender,
		ReceivedAt:     raw.ReceivedAt,
	}}

	return job
}

// DedupKey は重複判定キーを返す。案件 ID → URL → 本文ハッシュの順に使う。
// 判定キーを1本に絞ることで、重複判定を DB の UNIQUE 制約だけで完結させる。
//
// 案件 ID を URL より優先するのは、クラウドテックのようにメールへ案件詳細 URL を
// 持たず、エントリー先が全案件で共通のフォーム URL になるソースがあるため。
// 共通 URL を判定に使うと、別々の案件が同一の dedup_key になり、SaveJob の
// 「衝突したら既存行を更新する」仕様により先に保存した案件が上書きされて消える。
//
// ID にソース名を混ぜるのは、別のソースが同じ ID 体系（連番など）を使ったときに
// 無関係の案件どうしが衝突しないようにするため。
func DedupKey(sourceName, jobID, sourceURL, contentHash string) string {
	if id := strings.TrimSpace(jobID); id != "" {
		return "id:" + strings.TrimSpace(sourceName) + ":" + id
	}
	if u := strings.TrimSpace(sourceURL); u != "" {
		return "url:" + u
	}
	return "hash:" + contentHash
}

// ContentHash は案件名・企業名・本文から内容ハッシュを作る。
func ContentHash(title, company, body string) string {
	h := sha256.New()
	h.Write([]byte(title))
	h.Write([]byte{0})
	h.Write([]byte(company))
	h.Write([]byte{0})
	h.Write([]byte(body))
	return hex.EncodeToString(h.Sum(nil))
}

func emailMessageID(raw model.RawJob) string {
	if raw.Format == "email" {
		return raw.ExternalID
	}
	return ""
}

// dateLayouts は fixture で使う日付表記。上から順に試す。
var dateLayouts = []string{
	"2006-01-02",
	"2006/01/02",
	"2006年1月2日",
	"2006年01月02日",
	"2006-01",
	"2006/01",
	"2006年1月",
}

func parseDate(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	// 「2026年9月〜」「10月開始」のような装飾を落とす。
	s = strings.NewReplacer("〜", "", "～", "", "~", "", "開始", "", "から", "", "即", "").Replace(s)
	s = strings.TrimSpace(s)

	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	return nil
}
