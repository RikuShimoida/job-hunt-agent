// Package matching は案件をプロフィールに照らして除外・採点する。
//
// 値が unknown / nil の項目は「加点0・除外しない」で扱う。抽出漏れを理由に
// 良案件を落とすほうが、点が伸びずに埋もれるより損失が大きいため。
package matching

import (
	"fmt"
	"strings"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
)

// 配点。合計 100 点。
const (
	pointsRate        = 20
	pointsFullRemote  = 20
	pointsOnsiteOK    = 10
	pointsSkillsMax   = 25
	pointsRolesMax    = 10
	pointsStartDate   = 10
	pointsWorkDaysFit = 5
)

// Result は1案件の評価結果。
type Result struct {
	Score            int
	ScoreReasons     []string
	RejectionReasons []string
	Rejected         bool
}

// Evaluate は案件を評価する。必須条件に違反していれば Rejected=true とし、
// 違反理由を RejectionReasons に残す（点数は付けない）。
func Evaluate(job model.JobPosting, p model.Profile) Result {
	if rejections := reject(job, p); len(rejections) > 0 {
		return Result{
			Score:            0,
			RejectionReasons: rejections,
			Rejected:         true,
		}
	}

	var (
		score   int
		reasons []string
		demerit []string
	)

	// 単価
	switch {
	case job.RateMax == nil:
		demerit = append(demerit, "単価が読み取れなかった")
	case !comparableRate(job):
		demerit = append(demerit, fmt.Sprintf("月額換算できないため単価を比較できなかった（%s）", formatRate(job)))
	case p.TargetRate > 0 && *job.RateMax >= p.TargetRate:
		score += pointsRate
		reasons = append(reasons, fmt.Sprintf("希望単価以上（%s）", formatRate(job)))
	default:
		demerit = append(demerit, fmt.Sprintf("希望単価に届かない（%s）", formatRate(job)))
	}

	// リモート
	//
	// フルリモートに出社頻度分の 10 点も加えるのは、フルリモートを
	// 「出社0日 = 許容範囲内」とみなすため。こうしないと2つの配点が排他になり、
	// フルリモート案件が配点合計の 100 点に到達できなくなる。
	switch job.RemoteType {
	case model.RemoteTypeFullRemote:
		score += pointsFullRemote + pointsOnsiteOK
		reasons = append(reasons, "フルリモート")
	case model.RemoteTypeHybrid:
		if job.OnsiteDays != nil && *job.OnsiteDays <= p.MaxOnsiteDays {
			score += pointsOnsiteOK
			reasons = append(reasons, fmt.Sprintf("許容範囲の出社頻度（週%d日）", *job.OnsiteDays))
		} else {
			demerit = append(demerit, "出社頻度が許容範囲を超える")
		}
	case model.RemoteTypeOnsite:
		demerit = append(demerit, "常駐")
	case model.RemoteTypeUnknown:
		demerit = append(demerit, "リモート条件が読み取れなかった")
	}

	// 得意スキル
	jobSkills := append(append([]string{}, job.RequiredSkills...), job.PreferredSkills...)
	matchedSkills := intersect(jobSkills, p.PreferredSkills)
	if len(matchedSkills) > 0 && len(p.PreferredSkills) > 0 {
		pt := len(matchedSkills) * pointsSkillsMax / len(p.PreferredSkills)
		if pt > pointsSkillsMax {
			pt = pointsSkillsMax
		}
		score += pt
		reasons = append(reasons,
			fmt.Sprintf("得意スキル%d件一致（%s）", len(matchedSkills), strings.Join(matchedSkills, "、")))
	} else {
		demerit = append(demerit, "得意スキルとの一致なし")
	}

	// 役割
	matchedRoles := intersect(job.Roles, p.DesiredRoles)
	if len(matchedRoles) > 0 && len(p.DesiredRoles) > 0 {
		pt := len(matchedRoles) * pointsRolesMax / len(p.DesiredRoles)
		if pt > pointsRolesMax {
			pt = pointsRolesMax
		}
		score += pt
		reasons = append(reasons,
			fmt.Sprintf("希望する役割と一致（%s）", strings.Join(matchedRoles, "、")))
	}

	// 参画時期
	if job.StartDate != nil && p.AvailableFrom != nil {
		if !job.StartDate.Before(*p.AvailableFrom) {
			score += pointsStartDate
			reasons = append(reasons,
				fmt.Sprintf("参画希望時期と一致（%s〜）", job.StartDate.Format("2006-01-02")))
		} else {
			demerit = append(demerit, "開始時期が参画可能日より早い")
		}
	}

	// 稼働日数
	if job.WorkDaysMin != nil && job.WorkDaysMax != nil && p.PreferredWorkDays > 0 {
		if p.PreferredWorkDays >= *job.WorkDaysMin && p.PreferredWorkDays <= *job.WorkDaysMax {
			score += pointsWorkDaysFit
			reasons = append(reasons, fmt.Sprintf("希望稼働と一致（週%d日）", p.PreferredWorkDays))
		} else {
			demerit = append(demerit, "希望稼働と合わない")
		}
	}

	if score > 100 {
		score = 100
	}

	return Result{
		Score:            score,
		ScoreReasons:     reasons,
		RejectionReasons: demerit,
	}
}

// comparableRate は案件の単価が Profile の単価（月額）と直接比較できるかを返す。
//
// 時給案件を月額へ換算しないのは、換算に使う月間稼働時間が案件側の実稼働と
// 一致する保証がなく、換算値で除外すると良案件を取りこぼすため。
// 比較できないものは除外も加点もせず、減点理由として残す。
func comparableRate(job model.JobPosting) bool {
	return job.RateType == model.RateTypeMonthly
}

// reject は必須条件違反を返す。1つでもあれば案件は除外される。
func reject(job model.JobPosting, p model.Profile) []string {
	var out []string

	if job.RateMax != nil && comparableRate(job) &&
		p.MinimumRate > 0 && *job.RateMax < p.MinimumRate {
		out = append(out, fmt.Sprintf("最低希望単価を下回る（%s）", formatRate(job)))
	}
	if p.RemoteRequired && job.RemoteType == model.RemoteTypeOnsite {
		out = append(out, "フルリモート必須だが常駐案件")
	}
	if kw := matchedKeyword(job, p.ExcludedKeywords); kw != "" {
		out = append(out, fmt.Sprintf("避けたい条件に該当（%s）", kw))
	}
	if len(p.ContractTypes) > 0 && job.ContractType != "" &&
		!containsFold(p.ContractTypes, job.ContractType) {
		out = append(out, fmt.Sprintf("希望する契約形態ではない（%s）", job.ContractType))
	}
	return out
}

func matchedKeyword(job model.JobPosting, keywords []string) string {
	haystack := job.Title + "\n" + job.Summary + "\n" + job.RawText
	for _, kw := range keywords {
		kw = strings.TrimSpace(kw)
		if kw == "" {
			continue
		}
		if strings.Contains(haystack, kw) {
			return kw
		}
	}
	return ""
}

func intersect(a, b []string) []string {
	set := make(map[string]struct{}, len(b))
	for _, s := range b {
		set[strings.ToLower(strings.TrimSpace(s))] = struct{}{}
	}

	var out []string
	seen := make(map[string]struct{}, len(a))
	for _, s := range a {
		k := strings.ToLower(strings.TrimSpace(s))
		if _, ok := set[k]; !ok {
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, s)
	}
	return out
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(strings.TrimSpace(v), strings.TrimSpace(s)) {
			return true
		}
	}
	return false
}

func formatRate(job model.JobPosting) string {
	if job.RateMin == nil || job.RateMax == nil {
		return "不明"
	}
	unit := "円"
	if job.RateType == model.RateTypeHourly {
		unit = "円/時"
	}
	if *job.RateMin == *job.RateMax {
		return fmt.Sprintf("%d%s", *job.RateMin, unit)
	}
	return fmt.Sprintf("%d〜%d%s", *job.RateMin, *job.RateMax, unit)
}
