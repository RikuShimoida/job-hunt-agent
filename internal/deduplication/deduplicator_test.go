package deduplication_test

import (
	"testing"

	"github.com/RikuShimoida/job-hunt-agent/internal/deduplication"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
)

func job(dedupKey, sourceName, externalID string) model.JobPosting {
	return model.JobPosting{
		Title:    "案件",
		DedupKey: dedupKey,
		Sources: []model.JobSource{
			{SourceName: sourceName, ExternalID: externalID},
		},
	}
}

func TestDedupe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		input          []model.JobPosting
		wantJobs       int
		wantDuplicates int
		wantSources    int
	}{
		{
			name: "dedup_key が異なる案件はまとめない",
			input: []model.JobPosting{
				job("url:https://a.test/1", "email", "1"),
				job("url:https://a.test/2", "email", "2"),
			},
			wantJobs:       2,
			wantDuplicates: 0,
			wantSources:    1,
		},
		{
			name: "同じ dedup_key の案件は1件へまとめる",
			input: []model.JobPosting{
				job("url:https://a.test/1", "email", "1"),
				job("url:https://a.test/1", "email", "1"),
			},
			wantJobs:       1,
			wantDuplicates: 1,
			wantSources:    1,
		},
		{
			name: "同じ案件を別ソースが紹介したら紹介元を両方保持する",
			input: []model.JobPosting{
				job("url:https://a.test/1", "fixture-email", "mail-1"),
				job("url:https://a.test/1", "fixture-html", "page-1"),
			},
			wantJobs:       1,
			wantDuplicates: 1,
			wantSources:    2,
		},
		{
			name:           "空入力は空を返す",
			input:          nil,
			wantJobs:       0,
			wantDuplicates: 0,
			wantSources:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := deduplication.Dedupe(tt.input)

			if len(got.Jobs) != tt.wantJobs {
				t.Fatalf("Jobs = %d件, want %d件", len(got.Jobs), tt.wantJobs)
			}
			if got.DuplicateCount != tt.wantDuplicates {
				t.Errorf("DuplicateCount = %d, want %d", got.DuplicateCount, tt.wantDuplicates)
			}
			if tt.wantJobs > 0 && len(got.Jobs[0].Sources) != tt.wantSources {
				t.Errorf("Sources = %d件, want %d件 (%+v)",
					len(got.Jobs[0].Sources), tt.wantSources, got.Jobs[0].Sources)
			}
		})
	}
}

func TestDedupeKeepsFirstJobBody(t *testing.T) {
	t.Parallel()

	first := job("url:https://a.test/1", "email", "1")
	first.Title = "最初に現れた案件"

	second := job("url:https://a.test/1", "html", "2")
	second.Title = "後から現れた同じ案件"

	got := deduplication.Dedupe([]model.JobPosting{first, second})

	if len(got.Jobs) != 1 {
		t.Fatalf("Jobs = %d件, want 1件", len(got.Jobs))
	}
	if got.Jobs[0].Title != "最初に現れた案件" {
		t.Errorf("Title = %q, want 最初に現れた案件", got.Jobs[0].Title)
	}
}
