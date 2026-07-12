package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
)

// listSeparator はスライス項目を1カラムへ詰めるときの区切り。
// 案件名やスキル名に現れない制御文字を使い、値との衝突を避ける。
const listSeparator = "\x1f"

// Repository は SQLite への永続化。
type Repository struct {
	db *sql.DB
}

func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// SaveJob は案件を保存する。dedup_key が既存と衝突した場合は新規登録せず
// last_seen_at のみ更新し、created=false を返す。
func (r *Repository) SaveJob(ctx context.Context, job *model.JobPosting) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // Commit 済みなら no-op

	var existingID int64
	err = tx.QueryRowContext(ctx,
		"SELECT id FROM job_postings WHERE dedup_key = ?", job.DedupKey).Scan(&existingID)

	switch {
	case err == nil:
		if _, err := tx.ExecContext(ctx,
			"UPDATE job_postings SET last_seen_at = ? WHERE id = ?",
			job.LastSeenAt, existingID); err != nil {
			return false, fmt.Errorf("failed to update last_seen_at: %w", err)
		}
		job.ID = existingID
		if err := insertSources(ctx, tx, existingID, job.Sources); err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("failed to commit: %w", err)
		}
		return false, nil

	case errors.Is(err, sql.ErrNoRows):
		id, err := insertJob(ctx, tx, job)
		if err != nil {
			return false, err
		}
		job.ID = id
		if err := insertSources(ctx, tx, id, job.Sources); err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("failed to commit: %w", err)
		}
		return true, nil

	default:
		return false, fmt.Errorf("failed to look up dedup_key: %w", err)
	}
}

func insertJob(ctx context.Context, tx *sql.Tx, job *model.JobPosting) (int64, error) {
	const q = `INSERT INTO job_postings (
		title, company_name, summary, raw_text,
		rate_type, rate_min, rate_max, currency,
		work_days_min, work_days_max, monthly_hours_min, monthly_hours_max,
		remote_type, onsite_days, location,
		start_date, end_date, contract_type,
		required_skills, preferred_skills, roles,
		source_url, published_at, first_seen_at, last_seen_at,
		dedup_key, content_hash,
		status, score, score_reasons, rejection_reasons
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	res, err := tx.ExecContext(ctx, q,
		job.Title, job.CompanyName, job.Summary, job.RawText,
		string(job.RateType), job.RateMin, job.RateMax, job.Currency,
		job.WorkDaysMin, job.WorkDaysMax, job.MonthlyHoursMin, job.MonthlyHoursMax,
		string(job.RemoteType), job.OnsiteDays, job.Location,
		job.StartDate, job.EndDate, job.ContractType,
		encodeList(job.RequiredSkills), encodeList(job.PreferredSkills), encodeList(job.Roles),
		job.SourceURL, job.PublishedAt, job.FirstSeenAt, job.LastSeenAt,
		job.DedupKey, job.ContentHash,
		string(job.Status), job.Score,
		encodeList(job.ScoreReasons), encodeList(job.RejectionReasons),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to insert job: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get inserted id: %w", err)
	}
	return id, nil
}

func insertSources(ctx context.Context, tx *sql.Tx, jobID int64, sources []model.JobSource) error {
	const q = `INSERT OR IGNORE INTO job_sources (
		job_id, source_name, external_id, source_url, email_message_id, sender, received_at
	) VALUES (?, ?, ?, ?, ?, ?, ?)`

	for _, s := range sources {
		if _, err := tx.ExecContext(ctx, q,
			jobID, s.SourceName, s.ExternalID, s.SourceURL,
			s.EmailMessageID, s.Sender, s.ReceivedAt,
		); err != nil {
			return fmt.Errorf("failed to insert job source %s: %w", s.SourceName, err)
		}
	}
	return nil
}

// ListJobs は保存済みの案件をすべて返す（紹介元つき）。
func (r *Repository) ListJobs(ctx context.Context) (_ []model.JobPosting, err error) {
	const q = `SELECT
		id, title, company_name, summary, raw_text,
		rate_type, rate_min, rate_max, currency,
		work_days_min, work_days_max, monthly_hours_min, monthly_hours_max,
		remote_type, onsite_days, location,
		start_date, end_date, contract_type,
		required_skills, preferred_skills, roles,
		source_url, published_at, first_seen_at, last_seen_at,
		dedup_key, content_hash,
		status, score, score_reasons, rejection_reasons
	FROM job_postings ORDER BY id`

	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("failed to query jobs: %w", err)
	}
	defer func() { err = closeRows(rows, err) }()

	var jobs []model.JobPosting
	for rows.Next() {
		var (
			j                                      model.JobPosting
			rateType, remoteType, status           string
			requiredSkills, preferredSkills, roles string
			scoreReasons, rejectionReasons         string
		)
		if err := rows.Scan(
			&j.ID, &j.Title, &j.CompanyName, &j.Summary, &j.RawText,
			&rateType, &j.RateMin, &j.RateMax, &j.Currency,
			&j.WorkDaysMin, &j.WorkDaysMax, &j.MonthlyHoursMin, &j.MonthlyHoursMax,
			&remoteType, &j.OnsiteDays, &j.Location,
			&j.StartDate, &j.EndDate, &j.ContractType,
			&requiredSkills, &preferredSkills, &roles,
			&j.SourceURL, &j.PublishedAt, &j.FirstSeenAt, &j.LastSeenAt,
			&j.DedupKey, &j.ContentHash,
			&status, &j.Score, &scoreReasons, &rejectionReasons,
		); err != nil {
			return nil, fmt.Errorf("failed to scan job: %w", err)
		}
		j.RateType = model.RateType(rateType)
		j.RemoteType = model.RemoteType(remoteType)
		j.Status = model.JobStatus(status)
		j.RequiredSkills = decodeList(requiredSkills)
		j.PreferredSkills = decodeList(preferredSkills)
		j.Roles = decodeList(roles)
		j.ScoreReasons = decodeList(scoreReasons)
		j.RejectionReasons = decodeList(rejectionReasons)
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate jobs: %w", err)
	}

	for i := range jobs {
		sources, err := r.listSources(ctx, jobs[i].ID)
		if err != nil {
			return nil, err
		}
		jobs[i].Sources = sources
	}
	return jobs, nil
}

// closeRows は rows を閉じる。close 自体の失敗も握り潰さず、
// 先に起きたエラーがあればそれを優先して返す。
func closeRows(rows *sql.Rows, cause error) error {
	if err := rows.Close(); err != nil && cause == nil {
		return fmt.Errorf("failed to close rows: %w", err)
	}
	return cause
}

func (r *Repository) listSources(ctx context.Context, jobID int64) (_ []model.JobSource, err error) {
	const q = `SELECT id, job_id, source_name, external_id, source_url,
		email_message_id, sender, received_at
	FROM job_sources WHERE job_id = ? ORDER BY id`

	rows, err := r.db.QueryContext(ctx, q, jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to query job sources: %w", err)
	}
	defer func() { err = closeRows(rows, err) }()

	var sources []model.JobSource
	for rows.Next() {
		var s model.JobSource
		if err := rows.Scan(&s.ID, &s.JobID, &s.SourceName, &s.ExternalID,
			&s.SourceURL, &s.EmailMessageID, &s.Sender, &s.ReceivedAt); err != nil {
			return nil, fmt.Errorf("failed to scan job source: %w", err)
		}
		sources = append(sources, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate job sources: %w", err)
	}
	return sources, nil
}

// UpdateScore はスコアリング結果を反映する。
func (r *Repository) UpdateScore(ctx context.Context, job *model.JobPosting) error {
	const q = `UPDATE job_postings
		SET score = ?, score_reasons = ?, rejection_reasons = ?, status = ?
		WHERE id = ?`

	if _, err := r.db.ExecContext(ctx, q,
		job.Score, encodeList(job.ScoreReasons), encodeList(job.RejectionReasons),
		string(job.Status), job.ID,
	); err != nil {
		return fmt.Errorf("failed to update score for job %d: %w", job.ID, err)
	}
	return nil
}

// SaveRun は収集実行の結果を記録する。
func (r *Repository) SaveRun(ctx context.Context, run *model.CollectionRun) error {
	const q = `INSERT INTO collection_runs (
		source_name, started_at, finished_at, status,
		fetched_count, new_count, duplicate_count, error_message
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	res, err := r.db.ExecContext(ctx, q,
		run.SourceName, run.StartedAt, run.FinishedAt, string(run.Status),
		run.FetchedCount, run.NewCount, run.DuplicateCount, run.ErrorMessage,
	)
	if err != nil {
		return fmt.Errorf("failed to insert collection run for %s: %w", run.SourceName, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get inserted run id: %w", err)
	}
	run.ID = id
	return nil
}

// CountJobs は保存済み案件の件数を返す。重複登録が起きていないことの検証に使う。
func (r *Repository) CountJobs(ctx context.Context) (int, error) {
	var n int
	if err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM job_postings").Scan(&n); err != nil {
		return 0, fmt.Errorf("failed to count jobs: %w", err)
	}
	return n, nil
}

func encodeList(items []string) string {
	return strings.Join(items, listSeparator)
}

func decodeList(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, listSeparator)
}
