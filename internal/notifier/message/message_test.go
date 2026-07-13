package message_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/port"
	"github.com/RikuShimoida/job-hunt-agent/internal/notifier/message"
)

// newItem は新着（差分なし）の通知1件。
func newItem(job model.JobPosting) port.NotifyItem {
	return port.NotifyItem{Job: job}
}

// updateItem は prev から job へ重要変更があった再通知1件。
func updateItem(prev, job model.JobPosting) port.NotifyItem {
	return port.NotifyItem{Job: job, Update: true, PrevFields: message.Snapshot(prev)}
}

func fullJob() model.JobPosting {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	rateMin, rateMax := 750000, 850000
	days := 3

	return model.JobPosting{
		Title:            "Java／AWS 基盤改善案件",
		Score:            92,
		RateType:         model.RateTypeMonthly,
		RateMin:          &rateMin,
		RateMax:          &rateMax,
		WorkDaysMin:      &days,
		WorkDaysMax:      &days,
		RemoteType:       model.RemoteTypeFullRemote,
		Location:         "東京",
		StartDate:        &start,
		RequiredSkills:   []string{"Java", "Spring", "AWS", "Docker"},
		SourceURL:        "https://example.test/jobs/1",
		ScoreReasons:     []string{"希望単価以上", "フルリモート", "得意スキル4件一致"},
		RejectionReasons: []string{"Terraform実務経験が歓迎条件"},
		Sources: []model.JobSource{
			{SourceName: "レバテック"},
		},
	}
}

// TestFormatIncludesAllRequiredFields は、受入条件が求める全項目が
// 本文へ含まれることを確かめる。
func TestFormatIncludesAllRequiredFields(t *testing.T) {
	t.Parallel()

	out := message.Format(newItem(fullJob()))

	required := []string{
		"92点",
		"Java／AWS 基盤改善案件",
		"750000〜850000円",
		"週3日",
		"2026-09-01",
		"フルリモート",
		"東京",
		"Java、Spring、AWS、Docker",
		"レバテック",
		"希望単価以上",
		"Terraform実務経験が歓迎条件",
		"https://example.test/jobs/1",
	}
	for _, want := range required {
		if !strings.Contains(out, want) {
			t.Errorf("本文に %q が含まれていない\n--- 本文 ---\n%s", want, out)
		}
	}
}

func TestFormatDistinguishesNewAndUpdate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		item   port.NotifyItem
		want   string
		unwant string
	}{
		{
			name:   "未通知の案件は新着として出す",
			item:   newItem(fullJob()),
			want:   "【92点・新着】",
			unwant: "【92点・更新】",
		},
		{
			name:   "重要変更のあった既通知案件は更新として出す",
			item:   updateItem(fullJob(), fullJob()),
			want:   "【92点・更新】",
			unwant: "【92点・新着】",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out := message.Format(tt.item)
			if !strings.Contains(out, tt.want) {
				t.Errorf("見出しに %q が含まれていない: %q", tt.want, out)
			}
			if strings.Contains(out, tt.unwant) {
				t.Errorf("見出しに %q が含まれてしまっている: %q", tt.unwant, out)
			}
		})
	}
}

func TestFormatHandlesMissingValues(t *testing.T) {
	t.Parallel()

	job := model.JobPosting{
		Title:      "情報が少ない案件",
		Score:      60,
		RateType:   model.RateTypeUnknown,
		RemoteType: model.RemoteTypeUnknown,
	}

	out := message.Format(newItem(job))

	if !strings.Contains(out, "情報が少ない案件") {
		t.Errorf("案件名が出ていない: %q", out)
	}
	if strings.Count(out, "不明") < 3 {
		t.Errorf("未取得項目が「不明」として出ていない: %q", out)
	}
}

// TestFormatRate は単価表記を検証する。
//
// 片側だけ抽出できた単価を「不明」に丸めると、payload_hash（model.MaterialHash）は
// 変わるのに本文が前回と同一になり、中身の変わらない「更新」通知が飛ぶ。
// nil 判定は model.MaterialHash 側（両方 nil のときだけ「不明」）と揃える。
func TestFormatRate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		rateType model.RateType
		min      *int
		max      *int
		want     string
	}{
		{
			name:     "月額の範囲",
			rateType: model.RateTypeMonthly,
			min:      ptr(750000),
			max:      ptr(850000),
			want:     "750000〜850000円",
		},
		{
			name:     "上限と下限が同じなら1つだけ出す",
			rateType: model.RateTypeMonthly,
			min:      ptr(750000),
			max:      ptr(750000),
			want:     "750000円",
		},
		{
			name:     "上限だけ抽出できなければ下限からの表記にする",
			rateType: model.RateTypeMonthly,
			min:      ptr(750000),
			max:      nil,
			want:     "750000円〜",
		},
		{
			name:     "下限だけ抽出できなければ上限までの表記にする",
			rateType: model.RateTypeMonthly,
			min:      nil,
			max:      ptr(850000),
			want:     "〜850000円",
		},
		{
			name:     "両方とも抽出できなければ不明",
			rateType: model.RateTypeUnknown,
			min:      nil,
			max:      nil,
			want:     "不明",
		},
		{
			name:     "時給は単位を変える",
			rateType: model.RateTypeHourly,
			min:      ptr(5000),
			max:      ptr(5000),
			want:     "5000円/時",
		},
		{
			name:     "時給で上限だけ抽出できない",
			rateType: model.RateTypeHourly,
			min:      ptr(5000),
			max:      nil,
			want:     "5000円/時〜",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			job := model.JobPosting{
				Title:    "単価表記の案件",
				RateType: tt.rateType,
				RateMin:  tt.min,
				RateMax:  tt.max,
			}

			out := message.Format(newItem(job))
			if want := "単価：" + tt.want + "　"; !strings.Contains(out, want) {
				t.Errorf("本文に %q が含まれていない\n--- 本文 ---\n%s", want, out)
			}
		})
	}
}

// TestFormatRateDiffersOnOneSidedChange は、重要変更として検知される単価の変化が
// 本文にも現れることを確かめる（本文が同一のままの「更新」通知を防ぐ）。
func TestFormatRateDiffersOnOneSidedChange(t *testing.T) {
	t.Parallel()

	before := model.JobPosting{Title: "案件", RateType: model.RateTypeMonthly, RateMin: ptr(750000)}
	after := model.JobPosting{Title: "案件", RateType: model.RateTypeMonthly, RateMin: ptr(900000)}

	if model.MaterialHash(before) == model.MaterialHash(after) {
		t.Fatal("前提が崩れている: 片側だけの単価変更が重要変更として検知されていない")
	}
	// 見出し（新着 / 更新）以外に差が出ることを見るため、同じ新着として比べる。
	if message.Format(newItem(before)) == message.Format(newItem(after)) {
		t.Error("単価が変わったのに本文が同一（中身の変わらない「更新」通知になる）")
	}
}

func ptr(v int) *int { return &v }

func TestFormatHybridShowsOnsiteDays(t *testing.T) {
	t.Parallel()

	days := 1
	job := model.JobPosting{
		Title:      "ハイブリッド案件",
		RemoteType: model.RemoteTypeHybrid,
		OnsiteDays: &days,
	}

	if out := message.Format(newItem(job)); !strings.Contains(out, "ハイブリッド（週1日出社）") {
		t.Errorf("出社日数が出ていない: %q", out)
	}
}

// TestFormatShowsMaterialChanges は「更新」通知に変更前後の値が出ることを確かめる
// （Issue #13 の受入条件）。重要変更の4項目それぞれについて見る。
func TestFormatShowsMaterialChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*model.JobPosting)
		want   []string
		unwant []string
	}{
		{
			name: "単価が上がった",
			mutate: func(j *model.JobPosting) {
				j.RateMin, j.RateMax = ptr(900000), ptr(1000000)
			},
			want:   []string{"変更：", "・単価：750000〜850000円 → 900000〜1000000円"},
			unwant: []string{"・リモート：", "・開始時期：", "・必須スキル："},
		},
		{
			name: "フルリモートからハイブリッドへ変わった",
			mutate: func(j *model.JobPosting) {
				j.RemoteType, j.OnsiteDays = model.RemoteTypeHybrid, ptr(2)
			},
			want:   []string{"・リモート：フルリモート → ハイブリッド（週2日出社）"},
			unwant: []string{"・単価："},
		},
		{
			name: "開始時期が後ろ倒しになった",
			mutate: func(j *model.JobPosting) {
				start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
				j.StartDate = &start
			},
			want:   []string{"・開始時期：2026-09-01 → 2026-10-01"},
			unwant: []string{"・単価："},
		},
		{
			name: "必須スキルが増えた",
			mutate: func(j *model.JobPosting) {
				j.RequiredSkills = []string{"Java", "Spring", "AWS", "Docker", "Kubernetes"}
			},
			want:   []string{"・必須スキル：AWS、Docker、Java、Spring → AWS、Docker、Java、Kubernetes、Spring"},
			unwant: []string{"・単価："},
		},
		{
			name: "単価とリモートが同時に変わった",
			mutate: func(j *model.JobPosting) {
				j.RateMin, j.RateMax = ptr(900000), ptr(1000000)
				j.RemoteType, j.OnsiteDays = model.RemoteTypeOnsite, nil
			},
			want: []string{
				"・単価：750000〜850000円 → 900000〜1000000円",
				"・リモート：フルリモート → 常駐",
			},
			unwant: []string{"・開始時期："},
		},
		{
			name:   "重要変更が無ければ変更ブロックを出さない",
			mutate: func(*model.JobPosting) {},
			unwant: []string{"変更：", "・単価："},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			prev := fullJob()
			job := fullJob()
			tt.mutate(&job)

			out := message.Format(updateItem(prev, job))

			for _, want := range tt.want {
				if !strings.Contains(out, want) {
					t.Errorf("本文に %q が含まれていない\n--- 本文 ---\n%s", want, out)
				}
			}
			for _, unwant := range tt.unwant {
				if strings.Contains(out, unwant) {
					t.Errorf("本文に %q が含まれてしまっている\n--- 本文 ---\n%s", unwant, out)
				}
			}
		})
	}
}

// TestFormatNewJobHasNoChangeBlock は新着通知の本文が変わらないことを確かめる
// （受入条件「新着通知の本文は変わらない」）。
func TestFormatNewJobHasNoChangeBlock(t *testing.T) {
	t.Parallel()

	prev := fullJob()
	job := fullJob()
	job.RateMin, job.RateMax = ptr(900000), ptr(1000000)

	// 新着では PrevFields があっても差分を出さない（前回通知が存在しないため）。
	item := port.NotifyItem{Job: job, Update: false, PrevFields: message.Snapshot(prev)}

	if out := message.Format(item); strings.Contains(out, "変更：") {
		t.Errorf("新着通知に変更ブロックが出ている\n--- 本文 ---\n%s", out)
	}
}

// TestFormatWithoutPrevFieldsFallsBack は、スナップショットを持たない案件
// （マイグレーション前に通知済み）が見出しだけの「更新」通知になることを確かめる。
// 旧値を推測で埋めると、変わっていない項目まで変更として表示される。
func TestFormatWithoutPrevFieldsFallsBack(t *testing.T) {
	t.Parallel()

	item := port.NotifyItem{Job: fullJob(), Update: true}

	out := message.Format(item)
	if !strings.Contains(out, "【92点・更新】") {
		t.Errorf("更新の見出しが出ていない\n--- 本文 ---\n%s", out)
	}
	if strings.Contains(out, "変更：") {
		t.Errorf("旧値が無いのに変更ブロックが出ている\n--- 本文 ---\n%s", out)
	}
}

// TestSnapshotLabelsMatchMaterialFields は、差分表示の項目が重要変更の定義
// （model.MaterialFields）と一致していることを確かめる。
//
// model 側に5項目目が増えても Snapshot が追随しなければ、再通知はされるのに
// その項目の変更が本文へ出ないという食い違いが起きる。
func TestSnapshotLabelsMatchMaterialFields(t *testing.T) {
	t.Parallel()

	want := labelsOf(model.MaterialFields(fullJob()))
	got := labelsOf(message.Snapshot(fullJob()))

	if !slices.Equal(want, got) {
		t.Errorf("差分表示の項目が重要変更の定義とずれている: model=%v message=%v", want, got)
	}
}

func labelsOf(fields []string) []string {
	labels := make([]string, 0, len(fields))
	for _, f := range fields {
		label, _, _ := strings.Cut(f, "=")
		labels = append(labels, label)
	}
	return labels
}

func TestFormatFailures(t *testing.T) {
	t.Parallel()

	failures := []model.SourceFailure{
		{SourceName: "fixture-email", Message: "failed to read fixture dir"},
		{SourceName: "fixture-html", Message: "connection refused"},
	}

	out := message.FormatFailures(failures)

	for _, want := range []string{"収集エラー", "2件", "fixture-email", "failed to read fixture dir", "fixture-html", "connection refused"} {
		if !strings.Contains(out, want) {
			t.Errorf("本文に %q が含まれていない\n--- 本文 ---\n%s", want, out)
		}
	}
}
