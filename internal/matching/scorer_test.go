package matching_test

import (
	"strings"
	"testing"
	"time"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
	"github.com/RikuShimoida/job-hunt-agent/internal/matching"
	"github.com/RikuShimoida/job-hunt-agent/internal/normalization"
)

func profile() model.Profile {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	return model.Profile{
		SearchStatus:      model.SearchStatusSearching,
		AvailableFrom:     &from,
		MinimumRate:       700000,
		TargetRate:        800000,
		PreferredWorkDays: 3,
		RemoteRequired:    true,
		MaxOnsiteDays:     0,
		ContractTypes:     []string{"フリーランス", "業務委託"},
		RequiredSkills:    []string{"Java"},
		PreferredSkills:   []string{"Spring", "AWS", "Docker", "Terraform"},
		DesiredRoles:      []string{"バックエンド", "SRE"},
		ExcludedKeywords:  []string{"常駐必須"},
	}
}

// perfectJob は全条件を満たす案件（満点になる）。
func perfectJob() model.JobPosting {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	return model.JobPosting{
		Title:           "Java／AWS 基盤改善案件",
		RateType:        model.RateTypeMonthly,
		RateMin:         ptr(800000),
		RateMax:         ptr(850000),
		RemoteType:      model.RemoteTypeFullRemote,
		WorkDaysMin:     ptr(3),
		WorkDaysMax:     ptr(3),
		StartDate:       &start,
		ContractType:    "フリーランス",
		RequiredSkills:  []string{"Java", "Spring", "AWS"},
		PreferredSkills: []string{"Docker", "Terraform"},
		Roles:           []string{"バックエンド", "SRE"},
	}
}

func TestEvaluateRejection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		mutate        func(j *model.JobPosting)
		wantReasonSub string
	}{
		{
			name: "最低希望単価を下回る案件は除外する",
			mutate: func(j *model.JobPosting) {
				j.RateMin = ptr(500000)
				j.RateMax = ptr(600000)
			},
			wantReasonSub: "最低希望単価を下回る",
		},
		{
			name: "フルリモート必須なのに常駐案件は除外する",
			mutate: func(j *model.JobPosting) {
				j.RemoteType = model.RemoteTypeOnsite
			},
			wantReasonSub: "常駐案件",
		},
		{
			name: "避けたいキーワードを概要に含む案件は除外する",
			mutate: func(j *model.JobPosting) {
				j.Summary = "この案件は常駐必須です"
			},
			wantReasonSub: "避けたい条件に該当",
		},
		{
			name: "フルリモート必須なのにハイブリッド案件は除外する",
			mutate: func(j *model.JobPosting) {
				j.RemoteType = model.RemoteTypeHybrid
				j.OnsiteDays = ptr(1)
			},
			wantReasonSub: "出社を伴う案件",
		},
		{
			name: "希望しない契約形態の案件は除外する",
			mutate: func(j *model.JobPosting) {
				j.ContractType = "正社員"
			},
			wantReasonSub: "希望する契約形態ではない",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			job := perfectJob()
			tt.mutate(&job)

			got := matching.Evaluate(job, profile())

			if !got.Rejected {
				t.Fatalf("Rejected = false, want true (score=%d)", got.Score)
			}
			if got.Score != 0 {
				t.Errorf("除外された案件の Score = %d, want 0", got.Score)
			}
			if !containsSubstring(got.RejectionReasons, tt.wantReasonSub) {
				t.Errorf("RejectionReasons = %v, want to contain %q",
					got.RejectionReasons, tt.wantReasonSub)
			}
		})
	}
}

func TestEvaluateScore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(j *model.JobPosting)
		wantScore int
	}{
		{
			name:      "全条件を満たす案件は100点",
			mutate:    func(*model.JobPosting) {},
			wantScore: 100,
		},
		{
			name: "リモートが unknown だとフルリモート分の30点が付かない",
			mutate: func(j *model.JobPosting) {
				j.RemoteType = model.RemoteTypeUnknown
			},
			wantScore: 70,
		},
		{
			name: "単価が希望に届かないと20点が付かない",
			mutate: func(j *model.JobPosting) {
				j.RateMin = ptr(700000)
				j.RateMax = ptr(750000)
			},
			wantScore: 80,
		},
		{
			name: "単価が読み取れない場合も20点が付かない（除外はしない）",
			mutate: func(j *model.JobPosting) {
				j.RateType = model.RateTypeUnknown
				j.RateMin = nil
				j.RateMax = nil
			},
			wantScore: 80,
		},
		{
			name: "得意スキルが1件も一致しないと25点が付かない",
			mutate: func(j *model.JobPosting) {
				j.RequiredSkills = []string{"COBOL"}
				j.PreferredSkills = nil
			},
			wantScore: 75,
		},
		{
			name: "得意スキル4件中2件一致で12点（25点の按分）",
			mutate: func(j *model.JobPosting) {
				j.RequiredSkills = []string{"Java", "Spring"}
				j.PreferredSkills = []string{"AWS"}
			},
			wantScore: 87,
		},
		{
			name: "開始時期が参画可能日より早いと10点が付かない",
			mutate: func(j *model.JobPosting) {
				early := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
				j.StartDate = &early
			},
			wantScore: 90,
		},
		{
			name: "希望稼働と合わないと5点が付かない",
			mutate: func(j *model.JobPosting) {
				j.WorkDaysMin = ptr(5)
				j.WorkDaysMax = ptr(5)
			},
			wantScore: 95,
		},
		{
			name: "役割が一致しないと10点が付かない",
			mutate: func(j *model.JobPosting) {
				j.Roles = []string{"フロントエンド"}
			},
			wantScore: 90,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			job := perfectJob()
			tt.mutate(&job)

			got := matching.Evaluate(job, profile())

			if got.Rejected {
				t.Fatalf("Rejected = true, want false (reasons=%v)", got.RejectionReasons)
			}
			if got.Score != tt.wantScore {
				t.Errorf("Score = %d, want %d (reasons=%v)",
					got.Score, tt.wantScore, got.ScoreReasons)
			}
			if got.Score < 0 || got.Score > 100 {
				t.Errorf("Score = %d, want 0..100", got.Score)
			}
		})
	}
}

func TestEvaluateProducesReasons(t *testing.T) {
	t.Parallel()

	got := matching.Evaluate(perfectJob(), profile())

	if len(got.ScoreReasons) == 0 {
		t.Fatal("満点の案件なのに加点理由が空")
	}
	if !containsSubstring(got.ScoreReasons, "フルリモート") {
		t.Errorf("ScoreReasons = %v, want to contain フルリモート", got.ScoreReasons)
	}
	if !containsSubstring(got.ScoreReasons, "希望単価以上") {
		t.Errorf("ScoreReasons = %v, want to contain 希望単価以上", got.ScoreReasons)
	}
}

func TestEvaluateHybridWithinAllowedOnsiteDays(t *testing.T) {
	t.Parallel()

	p := profile()
	p.RemoteRequired = false
	p.MaxOnsiteDays = 1

	job := perfectJob()
	job.RemoteType = model.RemoteTypeHybrid
	job.OnsiteDays = ptr(1)

	got := matching.Evaluate(job, p)

	if got.Rejected {
		t.Fatalf("Rejected = true, want false (reasons=%v)", got.RejectionReasons)
	}
	// フルリモート30点の代わりに出社頻度10点だけが付くため 100 - 30 + 10 = 80。
	if got.Score != 80 {
		t.Errorf("Score = %d, want 80 (reasons=%v)", got.Score, got.ScoreReasons)
	}
	if !containsSubstring(got.ScoreReasons, "許容範囲の出社頻度") {
		t.Errorf("ScoreReasons = %v, want to contain 許容範囲の出社頻度", got.ScoreReasons)
	}
}

func TestEvaluateHybridExceedingAllowedOnsiteDays(t *testing.T) {
	t.Parallel()

	p := profile()
	p.RemoteRequired = false
	p.MaxOnsiteDays = 1

	job := perfectJob()
	job.RemoteType = model.RemoteTypeHybrid
	job.OnsiteDays = ptr(3)

	got := matching.Evaluate(job, p)

	if got.Rejected {
		t.Fatal("出社頻度超過は除外ではなく減点で扱うべき")
	}
	// リモート関連の加点がまったく付かないため 100 - 30 = 70。
	if got.Score != 70 {
		t.Errorf("Score = %d, want 70", got.Score)
	}
	if !containsSubstring(got.RejectionReasons, "出社頻度が許容範囲を超える") {
		t.Errorf("RejectionReasons = %v, want to contain 出社頻度が許容範囲を超える",
			got.RejectionReasons)
	}
}

// TestEvaluateExcludedKeywordScope は、除外キーワードの照合範囲が
// 案件名・概要に限られることを確かめる。
//
// メール原文（RawText）まで照合すると「常駐必須ではありません」のような
// 否定文や署名・引用に部分一致し、良案件が score=0 で通知から消える。
func TestEvaluateExcludedKeywordScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		mutate       func(j *model.JobPosting)
		wantRejected bool
	}{
		{
			name: "案件名に含まれる場合は除外する",
			mutate: func(j *model.JobPosting) {
				j.Title = "【常駐必須】Java 保守案件"
			},
			wantRejected: true,
		},
		{
			name: "概要に含まれる場合は除外する",
			mutate: func(j *model.JobPosting) {
				j.Summary = "常駐必須のためオフィス勤務となります"
			},
			wantRejected: true,
		},
		{
			name: "原文だけに含まれる否定文では除外しない",
			mutate: func(j *model.JobPosting) {
				j.RawText = "本案件は常駐必須ではありません。フルリモートで参画いただけます。"
			},
			wantRejected: false,
		},
		{
			name: "原文の署名・引用に含まれていても除外しない",
			mutate: func(j *model.JobPosting) {
				j.RawText = "--\n過去にご紹介した常駐必須案件の一覧はこちら"
			},
			wantRejected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			job := perfectJob()
			tt.mutate(&job)

			got := matching.Evaluate(job, profile())

			if got.Rejected != tt.wantRejected {
				t.Errorf("Rejected = %v, want %v (reasons=%v)",
					got.Rejected, tt.wantRejected, got.RejectionReasons)
			}
		})
	}
}

// TestEvaluateRemoteRequiredRejectsOnsiteAndHybrid は、remote_required が
// 「出社0日のみ許容」として解釈されることを確かめる。
//
// ハイブリッドを加点0で通すと、スキル・役割・時期・稼働の加点だけで
// 通知閾値（searching=60）を超え、フルリモート必須の利用者へ届いてしまう。
func TestEvaluateRemoteRequiredRejectsOnsiteAndHybrid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		remoteType   model.RemoteType
		onsiteDays   *int
		wantRejected bool
	}{
		{
			name:         "フルリモートは通す",
			remoteType:   model.RemoteTypeFullRemote,
			wantRejected: false,
		},
		{
			name:         "常駐は除外する",
			remoteType:   model.RemoteTypeOnsite,
			wantRejected: true,
		},
		{
			name:         "週1出社のハイブリッドも除外する",
			remoteType:   model.RemoteTypeHybrid,
			onsiteDays:   ptr(1),
			wantRejected: true,
		},
		{
			name:         "出社日数が読み取れないハイブリッドも除外する",
			remoteType:   model.RemoteTypeHybrid,
			wantRejected: true,
		},
		{
			name:         "リモート条件が読み取れない案件は除外しない（取りこぼしを避ける）",
			remoteType:   model.RemoteTypeUnknown,
			wantRejected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := profile()
			if !p.RemoteRequired {
				t.Fatal("前提が崩れている: profile() は RemoteRequired = true であるべき")
			}

			job := perfectJob()
			job.RemoteType = tt.remoteType
			job.OnsiteDays = tt.onsiteDays

			got := matching.Evaluate(job, p)

			if got.Rejected != tt.wantRejected {
				t.Errorf("Rejected = %v, want %v (score=%d, reasons=%v)",
					got.Rejected, tt.wantRejected, got.Score, got.RejectionReasons)
			}
		})
	}
}

// TestEvaluateRejectsBasicRemoteFromRealEmail は、実エージェントのメールにある
// 「基本リモート（必要に応じて出社あり）」という表記が、正規化を経て
// remote_required のプロフィールで確実に除外されることを固定する。
//
// 以前は normalization.Remote がこの表記をどのパターンにも当てられず unknown に
// 落としており、reject は unknown を除外しないため、フルリモート必須の利用者へ
// 出社を伴う案件がそのまま通知されていた。
func TestEvaluateRejectsBasicRemoteFromRealEmail(t *testing.T) {
	t.Parallel()

	p := profile()
	if !p.RemoteRequired {
		t.Fatal("前提が崩れている: profile() は RemoteRequired = true であるべき")
	}

	job := perfectJob()
	job.RemoteType, job.OnsiteDays = normalization.Remote("六本木駅 ※基本リモート（必要に応じて出社あり）")

	if job.RemoteType != model.RemoteTypeHybrid {
		t.Fatalf("RemoteType = %q, want %q", job.RemoteType, model.RemoteTypeHybrid)
	}

	got := matching.Evaluate(job, p)
	if !got.Rejected {
		t.Fatalf("Rejected = false, want true（score=%d, reasons=%v）", got.Score, got.ScoreReasons)
	}
	if got.Score != 0 {
		t.Errorf("Score = %d, want 0", got.Score)
	}
}

func TestEvaluateHourlyRateIsNotComparedToMonthlyMinimum(t *testing.T) {
	t.Parallel()

	// 5,000円/時 は月額 700,000円の最低単価と単位が違う。
	// 数値だけ比較すると 5000 < 700000 で除外されてしまうが、
	// 実際には 5,000円/時 × 140時間 = 70万円/月 で条件を満たしうる。
	job := perfectJob()
	job.RateType = model.RateTypeHourly
	job.RateMin = ptr(5000)
	job.RateMax = ptr(5000)

	got := matching.Evaluate(job, profile())

	if got.Rejected {
		t.Fatalf("時給案件が除外された（月額基準と比較してはならない）: %v", got.RejectionReasons)
	}
	// 単価の 20 点は付かないが、それ以外の 80 点は付く。
	if got.Score != 80 {
		t.Errorf("Score = %d, want 80（単価分の20点だけが付かない）", got.Score)
	}
	if !containsSubstring(got.RejectionReasons, "月額換算できない") {
		t.Errorf("RejectionReasons = %v, want to contain 月額換算できない", got.RejectionReasons)
	}
	if containsSubstring(got.ScoreReasons, "希望単価以上") {
		t.Errorf("比較できないはずの時給案件に単価の加点が付いた: %v", got.ScoreReasons)
	}
}

func ptr(v int) *int { return &v }

func containsSubstring(items []string, sub string) bool {
	for _, s := range items {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
