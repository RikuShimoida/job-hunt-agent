package model_test

import (
	"strings"
	"testing"
	"time"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
)

func intPtr(v int) *int { return &v }

func baseJob() model.JobPosting {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	return model.JobPosting{
		Title:          "Java／AWS 基盤改善案件",
		Score:          92,
		RateType:       model.RateTypeMonthly,
		RateMin:        intPtr(750000),
		RateMax:        intPtr(850000),
		RemoteType:     model.RemoteTypeFullRemote,
		StartDate:      &start,
		RequiredSkills: []string{"Java", "Spring", "AWS"},
		ScoreReasons:   []string{"希望単価以上", "フルリモート"},
	}
}

func TestMaterialHashDetectsOnlyMaterialChanges(t *testing.T) {
	t.Parallel()

	onsite := 2
	newStart := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		mutate         func(*model.JobPosting)
		wantHashChange bool
	}{
		{
			name:           "単価が変わればハッシュが変わる",
			mutate:         func(j *model.JobPosting) { j.RateMax = intPtr(1000000) },
			wantHashChange: true,
		},
		{
			name: "リモート頻度が変わればハッシュが変わる",
			mutate: func(j *model.JobPosting) {
				j.RemoteType = model.RemoteTypeHybrid
				j.OnsiteDays = &onsite
			},
			wantHashChange: true,
		},
		{
			name:           "開始時期が変わればハッシュが変わる",
			mutate:         func(j *model.JobPosting) { j.StartDate = &newStart },
			wantHashChange: true,
		},
		{
			name:           "必須スキルが増えればハッシュが変わる",
			mutate:         func(j *model.JobPosting) { j.RequiredSkills = []string{"Java", "Spring", "AWS", "Go"} },
			wantHashChange: true,
		},
		{
			name:           "必須スキルの並び順だけが変わってもハッシュは変わらない",
			mutate:         func(j *model.JobPosting) { j.RequiredSkills = []string{"AWS", "Java", "Spring"} },
			wantHashChange: false,
		},
		{
			name:           "加点理由の文言が変わってもハッシュは変わらない",
			mutate:         func(j *model.JobPosting) { j.ScoreReasons = []string{"希望単価以上（750000〜850000円）"} },
			wantHashChange: false,
		},
		{
			name:           "スコアが変わってもハッシュは変わらない",
			mutate:         func(j *model.JobPosting) { j.Score = 80 },
			wantHashChange: false,
		},
		{
			name:           "案件名が変わってもハッシュは変わらない",
			mutate:         func(j *model.JobPosting) { j.Title = "Java／AWS 基盤改善案件（急募）" },
			wantHashChange: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			before := baseJob()
			after := baseJob()
			tt.mutate(&after)

			changed := model.MaterialHash(before) != model.MaterialHash(after)
			if changed != tt.wantHashChange {
				t.Errorf("ハッシュの変化 = %v, want %v", changed, tt.wantHashChange)
			}

			hasChanges := len(model.MaterialChanges(before, after)) > 0
			if hasChanges != tt.wantHashChange {
				t.Errorf("MaterialChanges の有無 = %v, want %v（ハッシュと判定が食い違っている）",
					hasChanges, tt.wantHashChange)
			}
		})
	}
}

func TestMaterialHashIsStable(t *testing.T) {
	t.Parallel()

	job := baseJob()
	if model.MaterialHash(job) != model.MaterialHash(baseJob()) {
		t.Error("同じ内容の案件で MaterialHash が一致しない")
	}
}

func TestMaterialChangesDescribesBeforeAndAfter(t *testing.T) {
	t.Parallel()

	before := baseJob()
	after := baseJob()
	after.RateMin = intPtr(900000)
	after.RateMax = intPtr(1000000)

	changes := model.MaterialChanges(before, after)
	if len(changes) != 1 {
		t.Fatalf("MaterialChanges = %v, want 1件", changes)
	}

	got := changes[0]
	for _, want := range []string{"単価", "750000", "850000", "900000", "1000000"} {
		if !strings.Contains(got, want) {
			t.Errorf("変更の説明に %q が含まれていない: %q", want, got)
		}
	}
}

func TestMaterialChangesIsEmptyWhenUnchanged(t *testing.T) {
	t.Parallel()

	if changes := model.MaterialChanges(baseJob(), baseJob()); len(changes) != 0 {
		t.Errorf("MaterialChanges = %v, want 空（変更がない）", changes)
	}
}

// TestMaterialHashHandlesNilValues は、抽出できなかった項目（nil）でも
// ハッシュが算出でき、値が入った時点で変更として検出されることを確かめる。
func TestMaterialHashHandlesNilValues(t *testing.T) {
	t.Parallel()

	empty := model.JobPosting{RateType: model.RateTypeUnknown, RemoteType: model.RemoteTypeUnknown}

	filled := empty
	filled.RateType = model.RateTypeMonthly
	filled.RateMin = intPtr(700000)
	filled.RateMax = intPtr(800000)

	if model.MaterialHash(empty) == model.MaterialHash(filled) {
		t.Error("単価が nil から確定してもハッシュが変わらない")
	}
}
