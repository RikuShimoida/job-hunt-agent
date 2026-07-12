package parser_test

import (
	"strings"
	"testing"
	"time"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
	"github.com/RikuShimoida/job-hunt-agent/internal/parser"
)

func TestDedupKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		sourceURL   string
		contentHash string
		want        string
	}{
		{
			name:        "URL があれば URL を鍵にする",
			sourceURL:   "https://example.test/jobs/1",
			contentHash: "abc123",
			want:        "url:https://example.test/jobs/1",
		},
		{
			name:        "URL が無ければ本文ハッシュを鍵にする",
			sourceURL:   "",
			contentHash: "abc123",
			want:        "hash:abc123",
		},
		{
			name:        "空白だけの URL は無いものとして扱う",
			sourceURL:   "   ",
			contentHash: "abc123",
			want:        "hash:abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := parser.DedupKey(tt.sourceURL, tt.contentHash); got != tt.want {
				t.Errorf("DedupKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContentHashDistinguishesContent(t *testing.T) {
	t.Parallel()

	base := parser.ContentHash("案件A", "会社X", "本文")

	if same := parser.ContentHash("案件A", "会社X", "本文"); same != base {
		t.Errorf("同じ内容のハッシュが一致しない: %q != %q", same, base)
	}
	if diff := parser.ContentHash("案件B", "会社X", "本文"); diff == base {
		t.Error("案件名が違うのにハッシュが一致した")
	}
	if diff := parser.ContentHash("案件A", "会社Y", "本文"); diff == base {
		t.Error("企業名が違うのにハッシュが一致した")
	}
	if diff := parser.ContentHash("案件A", "会社X", "別の本文"); diff == base {
		t.Error("本文が違うのにハッシュが一致した")
	}
}

func TestBuild(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		raw    model.RawJob
		fields parser.Fields
		assert func(t *testing.T, job model.JobPosting)
	}{
		{
			name: "全項目が揃った案件を正規化する",
			raw: model.RawJob{
				SourceName: "fixture-email",
				Format:     "email",
				ExternalID: "fixture-001",
				Body:       "本文",
			},
			fields: parser.Fields{
				parser.FieldTitle:          "Java／AWS 基盤改善案件",
				parser.FieldCompany:        "架空テクノロジー",
				parser.FieldRate:           "75〜85万円",
				parser.FieldRemote:         "フルリモート",
				parser.FieldWorkDays:       "週3日",
				parser.FieldStartDate:      "2026-09-01",
				parser.FieldRequiredSkills: "Java、Spring、AWS",
				parser.FieldURL:            "https://example.test/jobs/1",
			},
			assert: func(t *testing.T, job model.JobPosting) {
				t.Helper()

				if job.Title != "Java／AWS 基盤改善案件" {
					t.Errorf("Title = %q", job.Title)
				}
				if job.RateType != model.RateTypeMonthly {
					t.Errorf("RateType = %q, want monthly", job.RateType)
				}
				if job.RateMin == nil || *job.RateMin != 750000 {
					t.Errorf("RateMin = %v, want 750000", job.RateMin)
				}
				if job.RemoteType != model.RemoteTypeFullRemote {
					t.Errorf("RemoteType = %q, want full_remote", job.RemoteType)
				}
				if job.StartDate == nil || job.StartDate.Format("2006-01-02") != "2026-09-01" {
					t.Errorf("StartDate = %v, want 2026-09-01", job.StartDate)
				}
				if len(job.RequiredSkills) != 3 {
					t.Errorf("RequiredSkills = %v, want 3 items", job.RequiredSkills)
				}
				if job.DedupKey != "url:https://example.test/jobs/1" {
					t.Errorf("DedupKey = %q", job.DedupKey)
				}
				if len(job.Sources) != 1 || job.Sources[0].EmailMessageID != "fixture-001" {
					t.Errorf("Sources = %+v, want email message id", job.Sources)
				}
			},
		},
		{
			name: "単価が無い案件は RateMin/RateMax が nil のまま保存される",
			raw: model.RawJob{
				SourceName: "fixture-email",
				Format:     "email",
				ExternalID: "fixture-004",
				Body:       "本文",
			},
			fields: parser.Fields{
				parser.FieldTitle:  "Go 基盤運用案件",
				parser.FieldRemote: "フルリモート",
			},
			assert: func(t *testing.T, job model.JobPosting) {
				t.Helper()

				if job.RateType != model.RateTypeUnknown {
					t.Errorf("RateType = %q, want unknown", job.RateType)
				}
				if job.RateMin != nil || job.RateMax != nil {
					t.Errorf("RateMin/RateMax = %v/%v, want nil/nil", job.RateMin, job.RateMax)
				}
			},
		},
		{
			name: "URL が無い案件は本文ハッシュを重複キーにする",
			raw: model.RawJob{
				SourceName: "fixture-email",
				Format:     "email",
				ExternalID: "fixture-005",
				Body:       "本文",
			},
			fields: parser.Fields{
				parser.FieldTitle: "URL なし案件",
			},
			assert: func(t *testing.T, job model.JobPosting) {
				t.Helper()

				if !strings.HasPrefix(job.DedupKey, "hash:") {
					t.Errorf("DedupKey = %q, want hash: prefix", job.DedupKey)
				}
				if job.ContentHash == "" {
					t.Error("ContentHash が空")
				}
			},
		},
		{
			name: "抽出項目が空でもパニックせず初期値で組み立てる",
			raw: model.RawJob{
				SourceName: "fixture-email",
				Format:     "email",
				Body:       "本文",
			},
			fields: parser.Fields{},
			assert: func(t *testing.T, job model.JobPosting) {
				t.Helper()

				if job.Title != "" {
					t.Errorf("Title = %q, want empty", job.Title)
				}
				if job.RemoteType != model.RemoteTypeUnknown {
					t.Errorf("RemoteType = %q, want unknown", job.RemoteType)
				}
				if job.Status != model.JobStatusNew {
					t.Errorf("Status = %q, want new", job.Status)
				}
				if job.RawText != "本文" {
					t.Errorf("RawText = %q, want 本文", job.RawText)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			job := parser.Build(tt.raw, tt.fields, now)
			tt.assert(t, job)
		})
	}
}
