package model_test

import (
	"testing"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
)

// TestHasContent は、案件として最低限のコンテンツ（案件名・企業名・単価の
// いずれか）を持つかの判定を確かめる。3項目すべてが取れないメールだけを
// スキップ対象（false）とする。
func TestHasContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		job  model.JobPosting
		want bool
	}{
		{
			name: "案件名・企業名・単価がすべて空ならコンテンツなし",
			job: model.JobPosting{
				RateType: model.RateTypeUnknown,
			},
			want: false,
		},
		{
			name: "案件名だけあればコンテンツあり",
			job: model.JobPosting{
				Title:    "Java／AWS 基盤改善案件",
				RateType: model.RateTypeUnknown,
			},
			want: true,
		},
		{
			name: "企業名だけあればコンテンツあり",
			job: model.JobPosting{
				CompanyName: "架空テクノロジー",
				RateType:    model.RateTypeUnknown,
			},
			want: true,
		},
		{
			name: "単価（月額）が取れていればコンテンツあり",
			job: model.JobPosting{
				RateType: model.RateTypeMonthly,
			},
			want: true,
		},
		{
			name: "単価（時給）が取れていてもコンテンツあり",
			job: model.JobPosting{
				RateType: model.RateTypeHourly,
			},
			want: true,
		},
		{
			name: "RateType がゼロ値（空文字）でも案件名・企業名が無ければコンテンツなし",
			job:  model.JobPosting{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.job.HasContent(); got != tt.want {
				t.Errorf("HasContent() = %v, want %v", got, tt.want)
			}
		})
	}
}
