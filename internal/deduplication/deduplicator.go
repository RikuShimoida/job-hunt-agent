// Package deduplication は1回の収集バッチ内の重複をまとめる。
//
// DB 側にも dedup_key の UNIQUE 制約があるが、バッチ内で先にまとめておくと
// 「同じ案件を複数エージェントが紹介している」ケースで紹介元を1件に取りこぼさず、
// 保存を1案件1回に収められる。
//
// Phase 1 の判定は dedup_key の完全一致のみ。案件名・単価・本文類似度による
// 判定は Phase 5 で追加する。
package deduplication

import "github.com/RikuShimoida/job-hunt-agent/internal/domain/model"

// Result は重複排除の結果。
type Result struct {
	Jobs           []model.JobPosting
	DuplicateCount int
}

// Dedupe は dedup_key が同じ案件を1件へまとめる。
// 案件本体は最初に現れたものを残し、紹介元（Sources）は全件を保持する。
func Dedupe(jobs []model.JobPosting) Result {
	out := make([]model.JobPosting, 0, len(jobs))
	index := make(map[string]int, len(jobs))
	duplicates := 0

	for _, job := range jobs {
		i, seen := index[job.DedupKey]
		if !seen {
			index[job.DedupKey] = len(out)
			out = append(out, job)
			continue
		}
		duplicates++
		out[i].Sources = mergeSources(out[i].Sources, job.Sources)
	}

	return Result{Jobs: out, DuplicateCount: duplicates}
}

// mergeSources は紹介元を重複なく足す。
func mergeSources(existing, incoming []model.JobSource) []model.JobSource {
	seen := make(map[string]struct{}, len(existing))
	for _, s := range existing {
		seen[sourceKey(s)] = struct{}{}
	}
	for _, s := range incoming {
		k := sourceKey(s)
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		existing = append(existing, s)
	}
	return existing
}

func sourceKey(s model.JobSource) string {
	return s.SourceName + "\x1f" + s.ExternalID
}
