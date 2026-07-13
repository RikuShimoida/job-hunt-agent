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

func TestFormatHourlyRate(t *testing.T) {
	t.Parallel()

	rate := 5000
	job := model.JobPosting{
		Title:    "時給案件",
		RateType: model.RateTypeHourly,
		RateMin:  &rate,
		RateMax:  &rate,
	}

	if out := message.Format(job, false); !strings.Contains(out, "5000円/時") {
		t.Errorf("時給表記が出ていない: %q", out)
	}
}

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
