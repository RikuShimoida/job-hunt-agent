CREATE TABLE IF NOT EXISTS job_postings (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    title             TEXT    NOT NULL,
    company_name      TEXT    NOT NULL DEFAULT '',
    summary           TEXT    NOT NULL DEFAULT '',
    raw_text          TEXT    NOT NULL DEFAULT '',

    rate_type         TEXT    NOT NULL DEFAULT 'unknown',
    rate_min          INTEGER,
    rate_max          INTEGER,
    currency          TEXT    NOT NULL DEFAULT 'JPY',

    work_days_min     INTEGER,
    work_days_max     INTEGER,
    monthly_hours_min INTEGER,
    monthly_hours_max INTEGER,

    remote_type       TEXT    NOT NULL DEFAULT 'unknown',
    onsite_days       INTEGER,
    location          TEXT    NOT NULL DEFAULT '',

    start_date        TIMESTAMP,
    end_date          TIMESTAMP,
    contract_type     TEXT    NOT NULL DEFAULT '',

    required_skills   TEXT    NOT NULL DEFAULT '',
    preferred_skills  TEXT    NOT NULL DEFAULT '',
    roles             TEXT    NOT NULL DEFAULT '',

    source_url        TEXT    NOT NULL DEFAULT '',
    published_at      TIMESTAMP,
    first_seen_at     TIMESTAMP NOT NULL,
    last_seen_at      TIMESTAMP NOT NULL,

    dedup_key         TEXT    NOT NULL,
    content_hash      TEXT    NOT NULL DEFAULT '',

    status            TEXT    NOT NULL DEFAULT 'new',
    score             INTEGER NOT NULL DEFAULT 0,
    score_reasons     TEXT    NOT NULL DEFAULT '',
    rejection_reasons TEXT    NOT NULL DEFAULT ''
);

-- 重複判定は dedup_key の一意制約だけで完結させる。
CREATE UNIQUE INDEX IF NOT EXISTS idx_job_postings_dedup_key ON job_postings (dedup_key);

CREATE TABLE IF NOT EXISTS job_sources (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id           INTEGER NOT NULL REFERENCES job_postings (id) ON DELETE CASCADE,
    source_name      TEXT    NOT NULL,
    external_id      TEXT    NOT NULL DEFAULT '',
    source_url       TEXT    NOT NULL DEFAULT '',
    email_message_id TEXT    NOT NULL DEFAULT '',
    sender           TEXT    NOT NULL DEFAULT '',
    received_at      TIMESTAMP
);

-- 同じ案件を同じソースが再提示しても紹介元は増やさない。
CREATE UNIQUE INDEX IF NOT EXISTS idx_job_sources_job_source
    ON job_sources (job_id, source_name, external_id);

CREATE TABLE IF NOT EXISTS notifications (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id       INTEGER NOT NULL REFERENCES job_postings (id) ON DELETE CASCADE,
    channel      TEXT    NOT NULL,
    sent_at      TIMESTAMP NOT NULL,
    payload_hash TEXT    NOT NULL DEFAULT '',
    result       TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS collection_runs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    source_name     TEXT    NOT NULL,
    started_at      TIMESTAMP NOT NULL,
    finished_at     TIMESTAMP NOT NULL,
    status          TEXT    NOT NULL,
    fetched_count   INTEGER NOT NULL DEFAULT 0,
    new_count       INTEGER NOT NULL DEFAULT 0,
    duplicate_count INTEGER NOT NULL DEFAULT 0,
    error_message   TEXT    NOT NULL DEFAULT ''
);
