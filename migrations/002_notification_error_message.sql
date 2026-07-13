-- 通知は送信試行ごとに1行 append する監査ログとして扱う。
-- 失敗した理由を残さないと「なぜ届かなかったか」を後から追えないため、
-- error_message を足す。UNIQUE 制約は張らない（失敗 → 再送の履歴を残すため）。
ALTER TABLE notifications ADD COLUMN error_message TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_notifications_job_id ON notifications (job_id);
