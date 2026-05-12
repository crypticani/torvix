CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TABLE IF NOT EXISTS cost_records
(
    id BIGSERIAL PRIMARY KEY,
    "timestamp" TIMESTAMPTZ NOT NULL,
    cloud_provider TEXT NOT NULL,
    account_id TEXT,
    service TEXT NOT NULL,
    category TEXT NOT NULL,
    resource_id TEXT,
    region TEXT,
    usage_quantity DOUBLE PRECISION,
    usage_unit TEXT,
    cost NUMERIC(20,8),
    currency TEXT,
    tags JSONB NOT NULL DEFAULT '{}'::jsonb,
    raw_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    source_object TEXT NOT NULL DEFAULT '',
    meter TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

SELECT create_hypertable('cost_records', 'timestamp', chunk_time_interval => INTERVAL '1 day', if_not_exists => TRUE, create_default_indexes => FALSE);

CREATE INDEX IF NOT EXISTS idx_cost_records_time_provider_service
    ON cost_records ("timestamp" DESC, cloud_provider, service);

CREATE INDEX IF NOT EXISTS idx_cost_records_account_time
    ON cost_records (account_id, "timestamp" DESC);

CREATE INDEX IF NOT EXISTS idx_cost_records_category_time
    ON cost_records (category, "timestamp" DESC);

CREATE INDEX IF NOT EXISTS idx_cost_records_region_time
    ON cost_records (region, "timestamp" DESC);

CREATE INDEX IF NOT EXISTS idx_cost_records_tags_gin
    ON cost_records USING GIN (tags);

CREATE INDEX IF NOT EXISTS idx_cost_records_raw_json_gin
    ON cost_records USING GIN (raw_json jsonb_path_ops);

ALTER TABLE cost_records SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'cloud_provider,account_id,service,category',
    timescaledb.compress_orderby = '"timestamp" DESC'
);

SELECT add_compression_policy('cost_records', INTERVAL '14 days', if_not_exists => TRUE);
SELECT add_retention_policy('cost_records', INTERVAL '365 days', if_not_exists => TRUE);
