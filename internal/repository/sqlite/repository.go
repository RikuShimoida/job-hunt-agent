package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/port"
)

// listSeparator はスライス項目を1カラムへ詰めるときの区切り。
// 案件名やスキル名に現れない制御文字を使い、値との衝突を避ける。
const listSeparator = "\x1f"

// jobColumns は job_postings の全カラム。SELECT と scanJob の並びを1箇所で揃える。
const jobColumns = `id, title, company_name, summary, raw_text,
	rate_type, rate_min, rate_max, currency,
	work_days_min, work_days_max, monthly_hours_min, monthly_hours_max,
	remote_type, onsite_days, location,
	start_date, end_date, contract_type,
	required_skills, preferred_skills, roles,
	source_url, apply_url, published_at, first_seen_at, last_seen_at,
	dedup_key, content_hash,
	status, score, score_reasons, rejection_reasons`

// Repository は SQLite への永続化。
type Repository struct {
	db *sql.DB
}

func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// SaveJob は案件を保存する。dedup_key が既存と衝突した場合は新規登録せず、
// 内容を最新の取得結果で更新し、重要変更の有無を返す。
//
// 衝突時に last_seen_at だけを更新しないのは、単価やリモート頻度が変わっても
// DB が古い値のまま残り、「変更を検知して再通知する」以前に変更が保存されないため。
func (r *Repository) SaveJob(ctx context.Context, job *model.JobPosting) (port.SaveResult, error) {
	var result port.SaveResult

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // Commit 済みなら no-op

	existing, err := findJobByDedupKey(ctx, tx, job.DedupKey)
	switch {
	case err == nil:
		result.MaterialChanges = model.MaterialChanges(existing, *job)

		// 採点結果は score コマンドの領分であり、収集で上書きしない。
		job.ID = existing.ID
		job.FirstSeenAt = existing.FirstSeenAt
		job.Status = existing.Status
		job.Score = existing.Score
		job.ScoreReasons = existing.ScoreReasons
		job.RejectionReasons = existing.RejectionReasons

		if err := updateJob(ctx, tx, job); err != nil {
			return result, err
		}

	case errors.Is(err, sql.ErrNoRows):
		id, err := insertJob(ctx, tx, job)
		if err != nil {
			return result, err
		}
		job.ID = id
		result.Created = true

	default:
		return result, fmt.Errorf("failed to look up dedup_key: %w", err)
	}

	if err := insertSources(ctx, tx, job.ID, job.Sources); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("failed to commit: %w", err)
	}
	return result, nil
}

func findJobByDedupKey(ctx context.Context, tx *sql.Tx, key string) (model.JobPosting, error) {
	row := tx.QueryRowContext(ctx,
		"SELECT "+jobColumns+" FROM job_postings WHERE dedup_key = ?", key)
	return scanJob(row)
}

// rowScanner は *sql.Row と *sql.Rows の共通部分。
type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(s rowScanner) (model.JobPosting, error) {
	var (
		j                                      model.JobPosting
		rateType, remoteType, status           string
		requiredSkills, preferredSkills, roles string
		scoreReasons, rejectionReasons         string
	)
	if err := s.Scan(
		&j.ID, &j.Title, &j.CompanyName, &j.Summary, &j.RawText,
		&rateType, &j.RateMin, &j.RateMax, &j.Currency,
		&j.WorkDaysMin, &j.WorkDaysMax, &j.MonthlyHoursMin, &j.MonthlyHoursMax,
		&remoteType, &j.OnsiteDays, &j.Location,
		&j.StartDate, &j.EndDate, &j.ContractType,
		&requiredSkills, &preferredSkills, &roles,
		&j.SourceURL, &j.ApplyURL, &j.PublishedAt, &j.FirstSeenAt, &j.LastSeenAt,
		&j.DedupKey, &j.ContentHash,
		&status, &j.Score, &scoreReasons, &rejectionReasons,
	); err != nil {
		return j, err
	}
	j.RateType = model.RateType(rateType)
	j.RemoteType = model.RemoteType(remoteType)
	j.Status = model.JobStatus(status)
	j.RequiredSkills = decodeList(requiredSkills)
	j.PreferredSkills = decodeList(preferredSkills)
	j.Roles = decodeList(roles)
	j.ScoreReasons = decodeList(scoreReasons)
	j.RejectionReasons = decodeList(rejectionReasons)
	return j, nil
}

func insertJob(ctx context.Context, tx *sql.Tx, job *model.JobPosting) (int64, error) {
	const q = `INSERT INTO job_postings (
		title, company_name, summary, raw_text,
		rate_type, rate_min, rate_max, currency,
		work_days_min, work_days_max, monthly_hours_min, monthly_hours_max,
		remote_type, onsite_days, location,
		start_date, end_date, contract_type,
		required_skills, preferred_skills, roles,
		source_url, apply_url, published_at, first_seen_at, last_seen_at,
		dedup_key, content_hash,
		status, score, score_reasons, rejection_reasons
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	res, err := tx.ExecContext(ctx, q,
		job.Title, job.CompanyName, job.Summary, job.RawText,
		string(job.RateType), job.RateMin, job.RateMax, job.Currency,
		job.WorkDaysMin, job.WorkDaysMax, job.MonthlyHoursMin, job.MonthlyHoursMax,
		string(job.RemoteType), job.OnsiteDays, job.Location,
		job.StartDate, job.EndDate, job.ContractType,
		encodeList(job.RequiredSkills), encodeList(job.PreferredSkills), encodeList(job.Roles),
		job.SourceURL, job.ApplyURL, job.PublishedAt, job.FirstSeenAt, job.LastSeenAt,
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

// updateJob は再収集した内容で既存行を更新する。
// first_seen_at と採点結果（status / score / *_reasons）は保持する。
func updateJob(ctx context.Context, tx *sql.Tx, job *model.JobPosting) error {
	const q = `UPDATE job_postings SET
		title = ?, company_name = ?, summary = ?, raw_text = ?,
		rate_type = ?, rate_min = ?, rate_max = ?, currency = ?,
		work_days_min = ?, work_days_max = ?, monthly_hours_min = ?, monthly_hours_max = ?,
		remote_type = ?, onsite_days = ?, location = ?,
		start_date = ?, end_date = ?, contract_type = ?,
		required_skills = ?, preferred_skills = ?, roles = ?,
		source_url = ?, apply_url = ?, published_at = ?, last_seen_at = ?,
		content_hash = ?
	WHERE id = ?`

	if _, err := tx.ExecContext(ctx, q,
		job.Title, job.CompanyName, job.Summary, job.RawText,
		string(job.RateType), job.RateMin, job.RateMax, job.Currency,
		job.WorkDaysMin, job.WorkDaysMax, job.MonthlyHoursMin, job.MonthlyHoursMax,
		string(job.RemoteType), job.OnsiteDays, job.Location,
		job.StartDate, job.EndDate, job.ContractType,
		encodeList(job.RequiredSkills), encodeList(job.PreferredSkills), encodeList(job.Roles),
		job.SourceURL, job.ApplyURL, job.PublishedAt, job.LastSeenAt,
		job.ContentHash,
		job.ID,
	); err != nil {
		return fmt.Errorf("failed to update job %d: %w", job.ID, err)
	}
	return nil
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
	rows, err := r.db.QueryContext(ctx, "SELECT "+jobColumns+" FROM job_postings ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("failed to query jobs: %w", err)
	}
	defer func() { err = closeRows(rows, err) }()

	var jobs []model.JobPosting
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job: %w", err)
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate jobs: %w", err)
	}

	// 紹介元は1クエリでまとめて引く。案件数ぶん listSources を発行する N+1 を避ける。
	sourcesByJob, err := r.listAllSources(ctx)
	if err != nil {
		return nil, err
	}
	for i := range jobs {
		jobs[i].Sources = sourcesByJob[jobs[i].ID]
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

// listAllSources は全案件の紹介元を1クエリで引き、job_id ごとにまとめて返す。
// job_id, id 昇順で読むため、各案件のスライスは id 昇順で並ぶ。
func (r *Repository) listAllSources(ctx context.Context) (_ map[int64][]model.JobSource, err error) {
	const q = `SELECT id, job_id, source_name, external_id, source_url,
		email_message_id, sender, received_at
	FROM job_sources ORDER BY job_id, id`

	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("failed to query job sources: %w", err)
	}
	defer func() { err = closeRows(rows, err) }()

	byJob := make(map[int64][]model.JobSource)
	for rows.Next() {
		var s model.JobSource
		if err := rows.Scan(&s.ID, &s.JobID, &s.SourceName, &s.ExternalID,
			&s.SourceURL, &s.EmailMessageID, &s.Sender, &s.ReceivedAt); err != nil {
			return nil, fmt.Errorf("failed to scan job source: %w", err)
		}
		byJob[s.JobID] = append(byJob[s.JobID], s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate job sources: %w", err)
	}
	return byJob, nil
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

// UpdateStatus は案件の処理段階だけを更新する。
func (r *Repository) UpdateStatus(ctx context.Context, jobID int64, status model.JobStatus) error {
	if _, err := r.db.ExecContext(ctx,
		"UPDATE job_postings SET status = ? WHERE id = ?", string(status), jobID,
	); err != nil {
		return fmt.Errorf("failed to update status for job %d: %w", jobID, err)
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

// SaveNotification は通知の送信試行を記録する。成功も失敗も1行として残す。
func (r *Repository) SaveNotification(ctx context.Context, n *model.Notification) error {
	const q = `INSERT INTO notifications (
		job_id, channel, sent_at, payload_hash, material_fields, result, error_message
	) VALUES (?, ?, ?, ?, ?, ?, ?)`

	res, err := r.db.ExecContext(ctx, q,
		n.JobID, n.Channel, n.SentAt, n.PayloadHash, encodeList(n.MaterialFields),
		string(n.Result), n.ErrorMessage,
	)
	if err != nil {
		return fmt.Errorf("failed to insert notification for job %d: %w", n.JobID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get inserted notification id: %w", err)
	}
	n.ID = id
	return nil
}

// ListNotifiedJobs は送信に成功した通知を job_id ごとに返す。
//
// 失敗行（result = failed）を含めないのは、送信できなかった案件を通知済み扱いにすると
// 次回実行で再送されず、取りこぼすため。
func (r *Repository) ListNotifiedJobs(ctx context.Context) (_ map[int64]port.NotifiedJob, err error) {
	// sent_at で並べないのは、アプリ側の時刻をテキストで保存しており
	// 辞書順が時刻順と一致する保証がないため。id は AUTOINCREMENT で単調増加する。
	const q = `SELECT job_id, payload_hash, material_fields FROM notifications
		WHERE result = ? ORDER BY id ASC`

	rows, err := r.db.QueryContext(ctx, q, string(model.NotificationResultSuccess))
	if err != nil {
		return nil, fmt.Errorf("failed to query notifications: %w", err)
	}
	defer func() { err = closeRows(rows, err) }()

	// 昇順に読んで上書きするため、同じ案件に複数行あれば最新の1件が残る。
	notified := make(map[int64]port.NotifiedJob)
	for rows.Next() {
		var (
			jobID  int64
			hash   string
			fields string
		)
		if err := rows.Scan(&jobID, &hash, &fields); err != nil {
			return nil, fmt.Errorf("failed to scan notification: %w", err)
		}
		notified[jobID] = port.NotifiedJob{
			PayloadHash:    hash,
			MaterialFields: decodeList(fields),
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate notifications: %w", err)
	}
	return notified, nil
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
