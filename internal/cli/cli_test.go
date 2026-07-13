package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/RikuShimoida/job-hunt-agent/internal/application"
)

// TestReportNotify は、送信失敗が終了コードへ出ることを確かめる。
//
// notify / run の RunE がこの関数の戻り値をそのまま返し、main が
// 非 nil のエラーで os.Exit(1) する。失敗を握り潰すと、定期実行が
// 成功扱いで終わり、通知が届いていないことに気づけない。
func TestReportNotify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		summary application.NotifySummary
		wantErr bool
	}{
		{
			name:    "通知対象が0件なら正常終了する",
			summary: application.NotifySummary{},
			wantErr: false,
		},
		{
			name:    "全件送信できたら正常終了する",
			summary: application.NotifySummary{TargetCount: 2, SentCount: 2},
			wantErr: false,
		},
		{
			name:    "1件でも送信に失敗したらエラーを返す",
			summary: application.NotifySummary{TargetCount: 3, SentCount: 2, FailedCount: 1},
			wantErr: true,
		},
		{
			name:    "全件失敗したらエラーを返す",
			summary: application.NotifySummary{TargetCount: 2, FailedCount: 2},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			err := reportNotify(&out, tt.summary)

			if tt.wantErr && !errors.Is(err, ErrNotifyFailed) {
				t.Errorf("err = %v, want ErrNotifyFailed でラップされていること", err)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("err = %v, want nil", err)
			}

			// 失敗時もサマリは出す（何件届いたのかが分からないと再実行の判断ができない）。
			if got := out.String(); !strings.Contains(got, "通知対象") {
				t.Errorf("サマリが出力されていない: %q", got)
			}
		})
	}
}
