package message_test

import (
	"strings"
	"testing"
	"time"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
	"github.com/RikuShimoida/job-hunt-agent/internal/notifier/message"
)

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

	out := message.Format(fullJob(), false)

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
		update bool
		want   string
		unwant string
	}{
		{
			name:   "未通知の案件は新着として出す",
			update: false,
			want:   "【92点・新着】",
			unwant: "【92点・更新】",
		},
		{
			name:   "重要変更のあった既通知案件は更新として出す",
			update: true,
			want:   "【92点・更新】",
			unwant: "【92点・新着】",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out := message.Format(fullJob(), tt.update)
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

	out := message.Format(job, false)

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

			out := message.Format(job, false)
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
	// 見出し（新着 / 更新）以外に差が出ることを見るため、同じ update で比べる。
	if message.Format(before, true) == message.Format(after, true) {
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

	if out := message.Format(job, false); !strings.Contains(out, "ハイブリッド（週1日出社）") {
		t.Errorf("出社日数が出ていない: %q", out)
	}
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
