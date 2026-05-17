CREATE TABLE IF NOT EXISTS processed_reports
(
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL,
    bucket TEXT NOT NULL,
    object_name TEXT NOT NULL,
    etag TEXT NOT NULL,
    last_modified TIMESTAMPTZ,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    record_count INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'processed',
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_processed_reports_identity
    ON processed_reports (provider, bucket, object_name, etag);

INSERT INTO processed_reports
    (provider, bucket, object_name, etag, last_modified, processed_at, record_count, status, error_message)
SELECT provider, bucket, object_name, etag, last_modified, processed_at, record_count, status, error_message
FROM processed_report_files
ON CONFLICT (provider, bucket, object_name, etag) DO NOTHING;

CREATE INDEX IF NOT EXISTS idx_processed_reports_processed_at
    ON processed_reports (processed_at DESC);

CREATE INDEX IF NOT EXISTS idx_processed_reports_status
    ON processed_reports (status);
