-- payload_hash は「変わったか」しか判らず、「何がどう変わったか」を復元できない。
-- 旧値は notify の時点で job_postings から上書き済みのため、通知時点のスナップショットを
-- 通知履歴側へ残す。既存行は '' となり、差分を出さず見出しだけの「更新」通知へフォールバックする。
ALTER TABLE notifications ADD COLUMN material_fields TEXT NOT NULL DEFAULT '';
