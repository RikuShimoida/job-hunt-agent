package message_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/port"
	"github.com/RikuShimoida/job-hunt-agent/internal/normalization"
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

// TestFormatShowsRecommendationAndConcerns は、通知本文が「推奨理由：」「懸念：」の
// 箇条書きを出すことを確かめる（Issue #21）。
func TestFormatShowsRecommendationAndConcerns(t *testing.T) {
	t.Parallel()

	out := message.Format(newItem(fullJob()))

	for _, want := range []string{
		"推奨理由：\n",
		"・希望単価以上\n",
		"・フルリモート\n",
		"懸念：\n",
		"・Terraform実務経験が歓迎条件\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("本文に %q が含まれていない\n--- 本文 ---\n%s", want, out)
		}
	}
	// 旧表記（1行へ「、」で連結する形式）へ戻っていないこと。
	for _, unwant := range []string{"加点：", "減点："} {
		if strings.Contains(out, unwant) {
			t.Errorf("本文に旧表記 %q が残っている\n--- 本文 ---\n%s", unwant, out)
		}
	}
}

// TestFormatConcernsIncludeExtractionMisses は、抽出できなかった項目が
// 「懸念：」へ挙がることと、二重に出ないことを確かめる（Issue #21）。
//
// 二重表示は matching.Evaluate と表示層の双方が抽出漏れを理由に持つと起きる。
// scorer 側は「希望と合わない理由」だけを持ち、抽出漏れの列挙はここへ集約している。
func TestFormatConcernsIncludeExtractionMisses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*model.JobPosting)
		want   []string
		unwant []string
	}{
		{
			name: "開始時期が抽出できなければ懸念に挙げる",
			mutate: func(j *model.JobPosting) {
				j.StartDate = nil
			},
			want:   []string{"懸念：", "・開始時期が案件情報に記載されていない"},
			unwant: []string{"・単価が案件情報に記載されていない"},
		},
		{
			name: "単価が抽出できなければ懸念に挙げる",
			mutate: func(j *model.JobPosting) {
				j.RateType, j.RateMin, j.RateMax = model.RateTypeUnknown, nil, nil
			},
			want:   []string{"・単価が案件情報に記載されていない"},
			unwant: []string{"・開始時期が案件情報に記載されていない"},
		},
		{
			name: "リモート条件が抽出できなければ懸念に挙げる",
			mutate: func(j *model.JobPosting) {
				j.RemoteType, j.OnsiteDays = model.RemoteTypeUnknown, nil
			},
			want:   []string{"・リモート条件が案件情報に記載されていない"},
			unwant: []string{"・稼働日数が案件情報に記載されていない"},
		},
		{
			name: "稼働日数が抽出できなければ懸念に挙げる",
			mutate: func(j *model.JobPosting) {
				j.WorkDaysMin, j.WorkDaysMax = nil, nil
			},
			want:   []string{"・稼働日数が案件情報に記載されていない"},
			unwant: []string{"・リモート条件が案件情報に記載されていない"},
		},
		{
			name:   "すべて抽出できていれば抽出漏れの懸念は出さない",
			mutate: func(*model.JobPosting) {},
			unwant: []string{"案件情報に記載されていない"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			job := fullJob()
			tt.mutate(&job)

			out := message.Format(newItem(job))

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
			// 同じ懸念が2度出ていないこと（scorer と表示層の二重計上を防ぐ）。
			for _, line := range tt.want {
				if strings.HasPrefix(line, "・") && strings.Count(out, line) > 1 {
					t.Errorf("懸念 %q が二重に出ている\n--- 本文 ---\n%s", line, out)
				}
			}
		})
	}
}

// TestFormatApplyURL は応募リンクの出力条件を確かめる（Issue #21）。
//
// 応募 URL（クラウドテック）と案件詳細 URL（フォスターネット）は別物で、
// ソースによってどちらか一方しか持たない。
func TestFormatApplyURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		applyURL  string
		sourceURL string
		want      []string
		unwant    []string
	}{
		{
			name:     "応募 URL があれば応募行を出す（クラウドテック）",
			applyURL: "https://share.hsforms.test/abc123",
			want:     []string{"応募：https://share.hsforms.test/abc123"},
			unwant:   []string{"URL："},
		},
		{
			name:      "案件詳細 URL があれば URL 行を出す（フォスターネット）",
			sourceURL: "https://example-agent.test/projects/detail/J00001",
			want:      []string{"URL：https://example-agent.test/projects/detail/J00001"},
			unwant:    []string{"応募："},
		},
		{
			name:      "両方あれば両方出す",
			applyURL:  "https://share.hsforms.test/abc123",
			sourceURL: "https://example-agent.test/projects/detail/J00001",
			want: []string{
				"応募：https://share.hsforms.test/abc123",
				"URL：https://example-agent.test/projects/detail/J00001",
			},
		},
		{
			name:   "どちらも無ければどちらの行も出さない",
			unwant: []string{"応募：", "URL："},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			job := fullJob()
			job.ApplyURL = tt.applyURL
			job.SourceURL = tt.sourceURL

			out := message.Format(newItem(job))

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

// TestUpdateAlwaysExplainsItself は本機能の不変条件を総当たりで検証する。
//
//	MaterialHash(before) != MaterialHash(after) ならば、
//	Format は必ず1行以上の差分を出す（＝再通知するなら必ず理由を本文に書ける）。
//
// ラベル集合の一致（TestSnapshotLabelsMatchMaterialFields）だけでは、値関数の
// 識別力の差を検知できない。実際、normalization.Remote が「週0日出社」に対して
// (full_remote, &0) を返していた頃は、ハッシュは変わるのに表示は「フルリモート」で
// 同一となり、見出し以外まったく同じ「更新」通知が飛んだ。
//
// 正規化が返しうる状態は有限なので、実際の正規化結果を突き合わせて総当たりする。
func TestUpdateAlwaysExplainsItself(t *testing.T) {
	t.Parallel()

	states := normalizedStates()

	for _, before := range states {
		for _, after := range states {
			if model.MaterialHash(before) == model.MaterialHash(after) {
				continue
			}

			out := message.Format(updateItem(before, after))
			if !strings.Contains(out, "変更：") {
				t.Errorf("重要変更ありと判定されたのに差分が出ていない\nbefore=%v\nafter =%v\n--- 本文 ---\n%s",
					model.MaterialFields(before), model.MaterialFields(after), out)
			}
		}
	}
}

// normalizedStates は normalization が実際に返しうる案件の状態を列挙する。
// 手で組んだ JobPosting ではなく正規化を通すのは、正規化の表現の揺れ
// （同じ意味が2通りで表現されること）ごと検証対象に入れるため。
func normalizedStates() []model.JobPosting {
	remoteInputs := []string{
		"", "応相談", "フルリモート", "完全リモート", "週0日出社",
		"リモート可", "リモート可（週1出社）", "週2日出社", "週5日出社", "常駐必須",
	}
	rateInputs := []string{
		"", "応相談", "75〜85万円", "90〜100万円", "80万円", "時給5000円",
	}
	skillInputs := [][]string{
		nil, {"Java", "AWS"}, {"AWS", "Java"}, {"Java", "AWS", "Kubernetes"},
	}
	startInputs := []*time.Time{
		nil,
		ptrTime(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)),
		ptrTime(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)),
	}

	var jobs []model.JobPosting
	for _, r := range remoteInputs {
		remoteType, onsiteDays := normalization.Remote(r)
		for _, rate := range rateInputs {
			rateType, rateMin, rateMax := normalization.Rate(rate)
			for _, skills := range skillInputs {
				for _, start := range startInputs {
					jobs = append(jobs, model.JobPosting{
						Title:          "案件",
						Score:          92,
						RateType:       rateType,
						RateMin:        rateMin,
						RateMax:        rateMax,
						RemoteType:     remoteType,
						OnsiteDays:     onsiteDays,
						StartDate:      start,
						RequiredSkills: normalization.Skills(skills),
					})
				}
			}
		}
	}
	return jobs
}

func ptrTime(t time.Time) *time.Time { return &t }

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
