-- Ingestion deduplication table (regular OLTP table, not a hypertable).
-- Tracks which cloud billing report files have already been processed to
-- prevent duplicate ingestion.
CREATE TABLE IF NOT EXISTS processed_report_files
(
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL,
    bucket TEXT NOT NULL,
    object_name TEXT NOT NULL,
    etag TEXT NOT NULL,
    last_modified TIMESTAMPTZ NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL,
    record_count BIGINT NOT NULL,
    status TEXT NOT NULL,
    error_message TEXT NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_processed_report_files_identity
    ON processed_report_files (provider, bucket, object_name, etag);

CREATE INDEX IF NOT EXISTS idx_processed_report_files_processed_at
    ON processed_report_files (processed_at DESC);

CREATE INDEX IF NOT EXISTS idx_processed_report_files_status
    ON processed_report_files (status);

CREATE MATERIALIZED VIEW IF NOT EXISTS cost_summary_daily
WITH (timescaledb.continuous) AS
SELECT
    time_bucket(INTERVAL '1 day', "timestamp") AS bucket,
    cloud_provider,
    account_id,
    service,
    category,
    currency,
    SUM(cost) AS total_cost,
    SUM(usage_quantity) AS total_usage
FROM cost_records
GROUP BY 1, 2, 3, 4, 5, 6
WITH NO DATA;

CREATE MATERIALIZED VIEW IF NOT EXISTS cost_summary_weekly
WITH (timescaledb.continuous) AS
SELECT
    time_bucket(INTERVAL '1 week', bucket) AS bucket,
    cloud_provider,
    account_id,
    service,
    category,
    currency,
    SUM(total_cost) AS total_cost,
    SUM(total_usage) AS total_usage
FROM cost_summary_daily
GROUP BY 1, 2, 3, 4, 5, 6
WITH NO DATA;

CREATE MATERIALIZED VIEW IF NOT EXISTS cost_summary_monthly
WITH (timescaledb.continuous) AS
SELECT
    time_bucket(INTERVAL '1 month', bucket) AS bucket,
    cloud_provider,
    account_id,
    service,
    category,
    currency,
    SUM(total_cost) AS total_cost,
    SUM(total_usage) AS total_usage
FROM cost_summary_daily
GROUP BY 1, 2, 3, 4, 5, 6
WITH NO DATA;

SELECT add_continuous_aggregate_policy(
    'cost_summary_daily',
    start_offset => INTERVAL '90 days',
    end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '30 minutes'
);

SELECT add_continuous_aggregate_policy(
    'cost_summary_weekly',
    start_offset => INTERVAL '365 days',
    end_offset => INTERVAL '1 day',
    schedule_interval => INTERVAL '2 hours'
);

SELECT add_continuous_aggregate_policy(
    'cost_summary_monthly',
    start_offset => INTERVAL '730 days',
    end_offset => INTERVAL '1 day',
    schedule_interval => INTERVAL '12 hours'
);
