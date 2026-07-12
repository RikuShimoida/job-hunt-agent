package stdout_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
	"github.com/RikuShimoida/job-hunt-agent/internal/notifier/stdout"
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

func TestNotifyIncludesAllRequiredFields(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	n := stdout.New(&buf)

	if err := n.Notify(context.Background(), []model.JobPosting{fullJob()}); err != nil {
		t.Fatalf("Notify() returned error: %v", err)
	}

	out := buf.String()

	// 受入条件: 案件名・スコア・主要条件・URL・加点理由・減点理由が含まれること。
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
			t.Errorf("出力に %q が含まれていない\n--- 出力 ---\n%s", want, out)
		}
	}
}

func TestNotifyWithNoJobs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	n := stdout.New(&buf)

	if err := n.Notify(context.Background(), nil); err != nil {
		t.Fatalf("Notify() returned error: %v", err)
	}

	if !strings.Contains(buf.String(), "通知対象の案件はありません") {
		t.Errorf("0件時のメッセージが出ていない: %q", buf.String())
	}
}

func TestFormatHandlesMissingValues(t *testing.T) {
	t.Parallel()

	// 単価・稼働・開始時期・リモートがすべて未取得の案件でも出力できること。
	job := model.JobPosting{
		Title:      "情報が少ない案件",
		Score:      60,
		RateType:   model.RateTypeUnknown,
		RemoteType: model.RemoteTypeUnknown,
	}

	out := stdout.Format(job)

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

	out := stdout.Format(job)
	if !strings.Contains(out, "5000円/時") {
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

	out := stdout.Format(job)
	if !strings.Contains(out, "ハイブリッド（週1日出社）") {
		t.Errorf("出社日数が出ていない: %q", out)
	}
}

func TestNotifyRespectsCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buf bytes.Buffer
	n := stdout.New(&buf)

	if err := n.Notify(ctx, []model.JobPosting{fullJob()}); err == nil {
		t.Fatal("キャンセル済み context でエラーが返らなかった")
	}
	if buf.Len() != 0 {
		t.Errorf("キャンセル済みなのに出力された: %q", buf.String())
	}
}
