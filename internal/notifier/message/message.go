// Package message は通知本文を組み立てる。
//
// 整形を stdout / slack のどちらかへ置くと、もう一方がそれを import することになり
// 通知実装どうしが依存し合う。共通の本文組み立てとして切り出している。
package message

import (
	"fmt"
	"slices"
	"strings"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/port"
)

// snapshotFields は差分表示に使う項目。
//
// model.MaterialFields をそのまま本文へ出さないのは、あれが payload_hash の入力であり、
// 値が `monthly 750000〜850000` のような内部表現だから。表示のために日本語化すると
// MaterialHash の入力が変わり、通知済みの案件が次回実行で一斉に「更新」再通知される。
// 再通知の判定（ハッシュ）と差分の表示（このスナップショット）を別々に持つことで、
// ハッシュの定義を固定したまま本文だけを読みやすくしている。
//
// 項目とラベルは model.MaterialFields と一致させる（message_test.go で担保）。
var snapshotFields = []struct {
	label string
	value func(model.JobPosting) string
}{
	{"単価", formatRate},
	{"リモート", formatRemote},
	{"開始時期", formatStart},
	{"必須スキル", formatRequiredSkills},
}

// snapshotSeparator はスナップショット1項目の「ラベル=値」を区切る。
const snapshotSeparator = "="

// Snapshot は通知時点の重要変更項目を表示用の文字列へ落とす。
// notifications.material_fields へ保存し、次に重要変更があったとき旧値として使う。
func Snapshot(job model.JobPosting) []string {
	out := make([]string, 0, len(snapshotFields))
	for _, f := range snapshotFields {
		out = append(out, f.label+snapshotSeparator+f.value(job))
	}
	return out
}

// Format は1案件ぶんの通知テキストを組み立てる。
// Update が true なら、既通知の案件に重要変更があった再通知として見出しを変え、
// 前回通知時点からの差分を本文へ載せる。
func Format(item port.NotifyItem) string {
	var b strings.Builder
	job := item.Job

	kind := "新着"
	if item.Update {
		kind = "更新"
	}

	fmt.Fprintf(&b, "【%d点・%s】%s\n", job.Score, kind, job.Title)

	// 差分を見出しの直下へ置くのは、更新通知で最も価値のある情報が「何が変わったか」だから。
	if item.Update {
		if changes := changedLines(item.PrevFields, job); len(changes) > 0 {
			b.WriteString("変更：\n")
			for _, c := range changes {
				fmt.Fprintf(&b, "・%s\n", c)
			}
		}
	}

	fmt.Fprintf(&b, "単価：%s　稼働：%s　開始：%s\n",
		formatRate(job), formatWorkDays(job), formatStart(job))
	fmt.Fprintf(&b, "勤務：%s　紹介元：%s\n", formatRemote(job), formatSources(job))

	if loc := strings.TrimSpace(job.Location); loc != "" {
		fmt.Fprintf(&b, "勤務地：%s\n", loc)
	}
	if skills := job.RequiredSkills; len(skills) > 0 {
		fmt.Fprintf(&b, "主要スキル：%s\n", strings.Join(skills, "、"))
	}
	if reasons := job.ScoreReasons; len(reasons) > 0 {
		b.WriteString("推奨理由：\n")
		for _, r := range reasons {
			fmt.Fprintf(&b, "・%s\n", r)
		}
	}
	if concerns := concerns(job); len(concerns) > 0 {
		b.WriteString("懸念：\n")
		for _, c := range concerns {
			fmt.Fprintf(&b, "・%s\n", c)
		}
	}

	// 応募 URL と案件詳細 URL は別物。クラウドテックは前者だけ、フォスターネットは
	// 後者だけを持つため、片方に寄せると一方のソースで応募導線が消える。
	if url := strings.TrimSpace(job.ApplyURL); url != "" {
		fmt.Fprintf(&b, "応募：%s\n", url)
	}
	if url := strings.TrimSpace(job.SourceURL); url != "" {
		fmt.Fprintf(&b, "URL：%s\n", url)
	}
	b.WriteString("\n")

	return b.String()
}

// missingFields は「抽出できなかった」ことを懸念として挙げる項目。
//
// 抽出漏れの列挙を matching.Evaluate 側へ持たせないのは、scorer が
// 「希望と合わない理由」を返し、表示層も同じ内容を足すと通知へ二重に出るため。
// 判定条件は本文の各フォーマッタが「不明」を出す条件と揃える（本文が「不明」なのに
// 懸念に挙がらない、あるいはその逆、という食い違いを防ぐ）。
var missingFields = []struct {
	missing func(model.JobPosting) bool
	reason  string
}{
	{func(j model.JobPosting) bool { return j.RateMin == nil && j.RateMax == nil }, "単価が案件情報に記載されていない"},
	{func(j model.JobPosting) bool { return j.StartDate == nil }, "開始時期が案件情報に記載されていない"},
	{func(j model.JobPosting) bool { return j.RemoteType == model.RemoteTypeUnknown }, "リモート条件が案件情報に記載されていない"},
	{func(j model.JobPosting) bool { return j.WorkDaysMin == nil || j.WorkDaysMax == nil }, "稼働日数が案件情報に記載されていない"},
}

// concerns は「希望と合わない理由」（scorer 由来）と「読み取れなかった項目」を並べる。
func concerns(job model.JobPosting) []string {
	out := make([]string, 0, len(job.RejectionReasons)+len(missingFields))
	out = append(out, job.RejectionReasons...)
	for _, f := range missingFields {
		if f.missing(job) {
			out = append(out, f.reason)
		}
	}
	return out
}

// changedLines は前回通知時点のスナップショットと現在の案件を突き合わせ、
// 変わった項目だけを「ラベル：旧値 → 新値」で返す。
//
// prev が空（マイグレーション前に通知した案件）なら差分を出さない。
// 旧値を推測で埋めると、変わっていない項目まで変更として表示されるため。
func changedLines(prev []string, job model.JobPosting) []string {
	if len(prev) == 0 {
		return nil
	}

	before := make(map[string]string, len(prev))
	for _, f := range prev {
		label, value, ok := strings.Cut(f, snapshotSeparator)
		if !ok {
			continue
		}
		before[label] = value
	}

	var lines []string
	for _, f := range snapshotFields {
		old, ok := before[f.label]
		if !ok {
			continue
		}
		if now := f.value(job); old != now {
			lines = append(lines, fmt.Sprintf("%s：%s → %s", f.label, old, now))
		}
	}
	return lines
}

// FormatFailures はソース取得失敗の通知テキストを組み立てる。
func FormatFailures(failures []model.SourceFailure) string {
	var b strings.Builder

	fmt.Fprintf(&b, "【収集エラー】%d件のソースで取得に失敗しました\n", len(failures))
	for _, f := range failures {
		fmt.Fprintf(&b, "・%s：%s\n", f.SourceName, f.Message)
	}
	return b.String()
}

// formatRate の nil 判定を model.materialRate（両方 nil のときだけ「不明」）へ揃えている。
// 片側 nil を「不明」に丸めると、単価が片側だけ変わった案件で payload_hash は変わるのに
// 本文は前回と同一になり、中身の変わらない「更新」通知が飛ぶ。
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

func formatWorkDays(job model.JobPosting) string {
	if job.WorkDaysMin == nil || job.WorkDaysMax == nil {
		return "不明"
	}
	if *job.WorkDaysMin == *job.WorkDaysMax {
		return fmt.Sprintf("週%d日", *job.WorkDaysMin)
	}
	return fmt.Sprintf("週%d〜%d日", *job.WorkDaysMin, *job.WorkDaysMax)
}

func formatStart(job model.JobPosting) string {
	if job.StartDate == nil {
		return "不明"
	}
	return job.StartDate.Format("2006-01-02")
}

func formatRemote(job model.JobPosting) string {
	switch job.RemoteType {
	case model.RemoteTypeFullRemote:
		return "フルリモート"
	case model.RemoteTypeHybrid:
		if job.OnsiteDays != nil {
			return fmt.Sprintf("ハイブリッド（週%d日出社）", *job.OnsiteDays)
		}
		return "ハイブリッド"
	case model.RemoteTypeOnsite:
		return "常駐"
	default:
		return "不明"
	}
}

// formatRequiredSkills は並び順の違いを変更と誤検知しないよう、複製をソートしてから連結する
// （model.materialSkills と同じ扱い）。本文の「主要スキル：」行は取得順のままにしている。
func formatRequiredSkills(job model.JobPosting) string {
	if len(job.RequiredSkills) == 0 {
		return "なし"
	}
	skills := slices.Clone(job.RequiredSkills)
	slices.Sort(skills)
	return strings.Join(skills, "、")
}

func formatSources(job model.JobPosting) string {
	if len(job.Sources) == 0 {
		return "不明"
	}
	names := make([]string, 0, len(job.Sources))
	seen := make(map[string]struct{}, len(job.Sources))
	for _, s := range job.Sources {
		if _, dup := seen[s.SourceName]; dup {
			continue
		}
		seen[s.SourceName] = struct{}{}
		names = append(names, s.SourceName)
	}
	return strings.Join(names, "、")
}
