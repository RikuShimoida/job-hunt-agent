package stdout_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/port"
	"github.com/RikuShimoida/job-hunt-agent/internal/notifier/stdout"
)

func fullJob() model.JobPosting {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	rateMin, rateMax := 750000, 850000
	days := 3

	return model.JobPosting{
		ID:               1,
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
		ScoreReasons:     []string{"希望単価以上", "フルリモート"},
		RejectionReasons: []string{"Terraform実務経験が歓迎条件"},
		Sources:          []model.JobSource{{SourceName: "レバテック"}},
	}
}

func TestNotifyWritesJobToWriter(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	n := stdout.New(&buf)

	records, err := n.Notify(context.Background(), []port.NotifyItem{{Job: fullJob()}})
	if err != nil {
		t.Fatalf("Notify() returned error: %v", err)
	}

	// dry-run を通知済みとして記録すると、実送信で「送ったつもり」の案件が生まれる。
	if len(records) != 0 {
		t.Errorf("送信記録 = %+v, want なし（dry-run は通知済みとして記録しない）", records)
	}

	out := buf.String()
	for _, want := range []string{"92点", "Java／AWS 基盤改善案件", "750000〜850000円", "URL："} {
		if !strings.Contains(out, want) {
			t.Errorf("出力に %q が含まれていない\n--- 出力 ---\n%s", want, out)
		}
	}
}

func TestNotifyMarksUpdate(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	n := stdout.New(&buf)

	if _, err := n.Notify(context.Background(),
		[]port.NotifyItem{{Job: fullJob(), Update: true}}); err != nil {
		t.Fatalf("Notify() returned error: %v", err)
	}

	if !strings.Contains(buf.String(), "・更新】") {
		t.Errorf("更新として出力されていない: %q", buf.String())
	}
}

func TestNotifyWithNoJobs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	n := stdout.New(&buf)

	if _, err := n.Notify(context.Background(), nil); err != nil {
		t.Fatalf("Notify() returned error: %v", err)
	}

	if !strings.Contains(buf.String(), "通知対象の案件はありません") {
		t.Errorf("0件時のメッセージが出ていない: %q", buf.String())
	}
}

func TestNotifyRespectsCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buf bytes.Buffer
	n := stdout.New(&buf)

	if _, err := n.Notify(ctx, []port.NotifyItem{{Job: fullJob()}}); err == nil {
		t.Fatal("キャンセル済み context でエラーが返らなかった")
	}
	if buf.Len() != 0 {
		t.Errorf("キャンセル済みなのに出力された: %q", buf.String())
	}
}

// TestErrorNotifierWritesFailures は、dry-run でもソース取得失敗が
// 「送信予定」として標準出力に出ることを確かめる。
func TestErrorNotifierWritesFailures(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	n := stdout.NewErrorNotifier(&buf)

	failures := []model.SourceFailure{
		{SourceName: "fixture-email", Message: "failed to read fixture dir"},
	}
	if err := n.NotifyError(context.Background(), failures); err != nil {
		t.Fatalf("NotifyError() returned error: %v", err)
	}

	out := buf.String()
	for _, want := range []string{"収集エラー", "fixture-email", "failed to read fixture dir"} {
		if !strings.Contains(out, want) {
			t.Errorf("出力に %q が含まれていない: %q", want, out)
		}
	}
}

func TestErrorNotifierIsSilentWithoutFailures(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	n := stdout.NewErrorNotifier(&buf)

	if err := n.NotifyError(context.Background(), nil); err != nil {
		t.Fatalf("NotifyError() returned error: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("失敗が無いのに出力された: %q", buf.String())
	}
}
