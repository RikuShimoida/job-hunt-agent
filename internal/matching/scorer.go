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
	//
	// 抽出できなかった項目をここで理由に挙げないのは、「読み取れなかった」の列挙を
	// 表示層（notifier/message）へ集約しているため。両方で持つと通知の「懸念：」へ
	// 同じ内容が二重に出る。scorer は「希望と合わない理由」だけを持つ。
	switch {
	case job.RateMax == nil:
	case !comparableRate(job):
		demerit = append(demerit, fmt.Sprintf("単価が%sで、月額換算できないため希望単価と比較できない", formatRate(job)))
	case p.TargetRate > 0 && *job.RateMax >= p.TargetRate:
		score += pointsRate
		reasons = append(reasons, fmt.Sprintf("希望単価 %s に到達している", formatRate(job)))
	default:
		demerit = append(demerit, fmt.Sprintf("単価が%sで、希望単価に届かない", formatRate(job)))
	}

	// リモート
	//
	// フルリモートに出社頻度分の 10 点も加えるのは、フルリモートを
	// 「出社0日 = 許容範囲内」とみなすため。こうしないと2つの配点が排他になり、
	// フルリモート案件が配点合計の 100 点に到達できなくなる。
	switch job.RemoteType {
	case model.RemoteTypeFullRemote:
		score += pointsFullRemote + pointsOnsiteOK
		reasons = append(reasons, "フルリモートで出社が不要")
	case model.RemoteTypeHybrid:
		// 出社日数が nil のとき demerit を積まないのは、「超過が確定した」と断定すると
		// 本文（message.formatRemote は日数 nil を「不明」表示にする）と食い違うため。
		// 出社日数不明の懸念は表示層（notifier/message）が「記載されていない」として挙げる。
		switch {
		case job.OnsiteDays == nil:
			// 出社日数不明。加点も減点もしない（表示層が「記載されていない」を懸念に挙げる）。
		case *job.OnsiteDays <= p.MaxOnsiteDays:
			score += pointsOnsiteOK
			reasons = append(reasons, fmt.Sprintf("出社は週%d日で、許容範囲の出社頻度に収まる", *job.OnsiteDays))
		default:
			demerit = append(demerit, "出社頻度が許容範囲を超える")
		}
	case model.RemoteTypeOnsite:
		demerit = append(demerit, "常駐案件で出社が必要")
	case model.RemoteTypeUnknown:
		// リモート条件不明。加点も減点もしない（表示層が「記載されていない」を懸念に挙げる）。
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
			fmt.Sprintf("得意スキルの %s が一致する", strings.Join(matchedSkills, "、")))
	} else {
		demerit = append(demerit, "得意スキルと一致するものがない")
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
			fmt.Sprintf("希望する役割の %s に合う", strings.Join(matchedRoles, "、")))
	}

	// 参画時期
	if job.StartDate != nil && p.AvailableFrom != nil {
		if !job.StartDate.Before(*p.AvailableFrom) {
			score += pointsStartDate
			reasons = append(reasons,
				fmt.Sprintf("%s 開始で、参画希望時期に間に合う", job.StartDate.Format("2006-01-02")))
		} else {
			demerit = append(demerit,
				fmt.Sprintf("%s 開始で、参画可能日より早い", job.StartDate.Format("2006-01-02")))
		}
	}

	// 稼働日数
	if job.WorkDaysMin != nil && job.WorkDaysMax != nil && p.PreferredWorkDays > 0 {
		if p.PreferredWorkDays >= *job.WorkDaysMin && p.PreferredWorkDays <= *job.WorkDaysMax {
			score += pointsWorkDaysFit
			reasons = append(reasons,
				fmt.Sprintf("週%d日で稼働でき、希望する稼働日数に合う", p.PreferredWorkDays))
		} else {
			demerit = append(demerit,
				fmt.Sprintf("案件の稼働日数が%sで、希望する週%d日と合わない",
					formatWorkDays(job), p.PreferredWorkDays))
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
	// remote_required は「出社0日のみ許容」と解釈し、ハイブリッドも除外する。
	// 加点0で通さないのは、スキル・役割・時期・稼働の加点だけで通知閾値を超え、
	// フルリモート必須の利用者へ出社ありの案件が届いてしまうため。
	// 出社を許容する運用は remote_required: false + max_onsite_days: N で表現する
	// （ValidateProfile が両者の同時指定を禁じているのと整合する）。
	if p.RemoteRequired {
		switch job.RemoteType {
		case model.RemoteTypeOnsite:
			out = append(out, "フルリモート必須だが常駐案件")
		case model.RemoteTypeHybrid:
			out = append(out, "フルリモート必須だが出社を伴う案件")
		case model.RemoteTypeFullRemote, model.RemoteTypeUnknown:
		}
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

// matchedKeyword は除外キーワードに該当する語を返す。
//
// 照合対象へ RawText を含めないのは、原文全体には「常駐必須ではありません」の
// ような否定文や署名・引用が混ざり、部分一致で良案件を誤除外するため。
// 除外は score=0 の終端判定であり、誤除外の損失が取りこぼしより大きい。
func matchedKeyword(job model.JobPosting, keywords []string) string {
	haystack := job.Title + "\n" + job.Summary
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

// formatWorkDays は稼働日数を表記する。
//
// 呼び出し元（Evaluate の稼働日数ブロック）は両端の非 nil を確かめてから呼ぶため、
// 冒頭の nil ガードは防御的フォールバックにすぎない（通常は「不明」を返さない）。
func formatWorkDays(job model.JobPosting) string {
	if job.WorkDaysMin == nil || job.WorkDaysMax == nil {
		return "不明"
	}
	if *job.WorkDaysMin == *job.WorkDaysMax {
		return fmt.Sprintf("週%d日", *job.WorkDaysMin)
	}
	return fmt.Sprintf("週%d〜%d日", *job.WorkDaysMin, *job.WorkDaysMax)
}

// formatRate の nil 判定は notifier/message.formatRate と揃える（両端 nil のときだけ「不明」）。
// 片側 nil を「不明」に丸めると、本文が `単価：〜850000円` なのに推奨理由が
// 「希望単価 不明 に到達している」となり、通知内で単価表記が食い違う。
func formatRate(job model.JobPosting) string {
	if job.RateMin == nil && job.RateMax == nil {
		return "不明"
	}
	unit := "円"
	if job.RateType == model.RateTypeHourly {
		unit = "円/時"
	}
	switch {
	case job.RateMax == nil:
		return fmt.Sprintf("%d%s〜", *job.RateMin, unit)
	case job.RateMin == nil:
		return fmt.Sprintf("〜%d%s", *job.RateMax, unit)
	case *job.RateMin == *job.RateMax:
		return fmt.Sprintf("%d%s", *job.RateMin, unit)
	default:
		return fmt.Sprintf("%d〜%d%s", *job.RateMin, *job.RateMax, unit)
	}
}
