-- 応募（エントリー）フォームの URL。source_url とは別カラムに持つ。
--
-- source_url へ相乗りさせないのは、応募 URL が提携企業ごとに共通で案件ごとに一意でないため。
-- dedup_key は source_url を "url:" 鍵に使うので、共通 URL が流れ込むと同一企業の複数案件が
-- 同じ鍵になり、SaveJob の「衝突したら既存行を更新する」仕様で先に保存した案件が消える。
--
-- 既存行は '' となり、通知の「応募：」行が出ないだけで挙動は変わらない。
ALTER TABLE job_postings ADD COLUMN apply_url TEXT NOT NULL DEFAULT '';
