package matching

import (
	"testing"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
)

// TestFormatRateMatchesMessageLayer は、matching.formatRate の nil 判定が
// notifier/message.formatRate と揃っていることを固定する（両端 nil のときだけ「不明」）。
//
// 片側 nil を「不明」に丸めると、本文が「単価：〜850000円」なのに推奨理由が
// 「希望単価 不明 に到達している」となり、同じ通知の中で単価表記が食い違う。
func TestFormatRateMatchesMessageLayer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		rateType model.RateType
		min      *int
		max      *int
		want     string
	}{
		{"月額の範囲", model.RateTypeMonthly, iptr(750000), iptr(850000), "750000〜850000円"},
		{"上限と下限が同じなら1つだけ", model.RateTypeMonthly, iptr(800000), iptr(800000), "800000円"},
		{"上限だけ nil は下限からの表記（不明にしない）", model.RateTypeMonthly, iptr(750000), nil, "750000円〜"},
		{"下限だけ nil は上限までの表記（不明にしない）", model.RateTypeMonthly, nil, iptr(850000), "〜850000円"},
		{"両端 nil のときだけ不明", model.RateTypeUnknown, nil, nil, "不明"},
		{"時給は単位を変える", model.RateTypeHourly, iptr(5000), iptr(5000), "5000円/時"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := formatRate(model.JobPosting{
				RateType: tt.rateType,
				RateMin:  tt.min,
				RateMax:  tt.max,
			})
			if got != tt.want {
				t.Errorf("formatRate() = %q, want %q", got, tt.want)
			}
		})
	}
}

func iptr(v int) *int { return &v }
