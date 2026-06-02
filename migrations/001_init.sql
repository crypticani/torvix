CREATE EXTENSION IF NOT EXISTS timescaledb;

-- Analytical hypertable: append-only billing records.
-- No PRIMARY KEY — TimescaleDB partitions by "timestamp" and unique
-- constraints must include the partition column, which is unnecessary
-- for an analytical/FinOps workload.
CREATE TABLE IF NOT EXISTS cost_records
(
    id BIGSERIAL,
    "timestamp" TIMESTAMPTZ NOT NULL,
    cloud_provider TEXT NOT NULL,
    account_id TEXT,
    service TEXT NOT NULL,
    category TEXT NOT NULL,
    billing_scope_type TEXT NOT NULL DEFAULT '',
    billing_scope_id TEXT NOT NULL DEFAULT '',
    billing_scope_name TEXT NOT NULL DEFAULT '',
    project_id TEXT NOT NULL DEFAULT '',
    project_name TEXT NOT NULL DEFAULT '',
    project_source TEXT NOT NULL DEFAULT '',
    network_scope_type TEXT NOT NULL DEFAULT '',
    network_scope_id TEXT NOT NULL DEFAULT '',
    network_scope_name TEXT NOT NULL DEFAULT '',
    resource_id TEXT,
    resource_type TEXT NOT NULL DEFAULT '',
    region TEXT,
    usage_quantity DOUBLE PRECISION,
    usage_unit TEXT,
    cost NUMERIC(20,8),
    currency TEXT,
    tags JSONB NOT NULL DEFAULT '{}'::jsonb,
    raw_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    raw_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    source_object TEXT NOT NULL DEFAULT '',
    meter TEXT NOT NULL DEFAULT '',
    record_type TEXT NOT NULL DEFAULT '',
    source_file_key TEXT NOT NULL DEFAULT '',
    source_file_etag TEXT NOT NULL DEFAULT '',
    source_line_number BIGINT NOT NULL DEFAULT 0,
    source_record_hash TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

SELECT create_hypertable('cost_records', 'timestamp', chunk_time_interval => INTERVAL '1 day', if_not_exists => TRUE, create_default_indexes => FALSE);

-- Optimized indexes for analytical queries.
-- Each leading column is a common filter dimension; timestamp is always
-- included for chunk exclusion during query planning.
CREATE INDEX IF NOT EXISTS idx_cost_records_timestamp
    ON cost_records ("timestamp" DESC);

CREATE INDEX IF NOT EXISTS idx_cost_records_provider_time
    ON cost_records (cloud_provider, "timestamp" DESC);

CREATE INDEX IF NOT EXISTS idx_cost_records_service_time
    ON cost_records (service, "timestamp" DESC);

CREATE INDEX IF NOT EXISTS idx_cost_records_category_time
    ON cost_records (category, "timestamp" DESC);

CREATE INDEX IF NOT EXISTS idx_cost_records_region_time
    ON cost_records (region, "timestamp" DESC);

CREATE INDEX IF NOT EXISTS idx_cost_records_account_time
    ON cost_records (account_id, "timestamp" DESC);

CREATE INDEX IF NOT EXISTS idx_cost_records_billing_scope_time
    ON cost_records (billing_scope_type, billing_scope_id, "timestamp" DESC);

CREATE INDEX IF NOT EXISTS idx_cost_records_source_object_time
    ON cost_records (cloud_provider, source_object, "timestamp" DESC);

CREATE INDEX IF NOT EXISTS idx_cost_records_tags_gin
    ON cost_records USING GIN (tags);

CREATE INDEX IF NOT EXISTS idx_cost_records_raw_json_gin
    ON cost_records USING GIN (raw_json jsonb_path_ops);

CREATE INDEX IF NOT EXISTS idx_cost_records_raw_metadata_gin
    ON cost_records USING GIN (raw_metadata jsonb_path_ops);

CREATE UNIQUE INDEX IF NOT EXISTS idx_cost_records_aws_idempotency
    ON cost_records ("timestamp", cloud_provider, region, billing_scope_type, billing_scope_id, service, record_type)
    WHERE cloud_provider = 'aws';

CREATE UNIQUE INDEX IF NOT EXISTS idx_cost_records_aws_cur_record_hash
    ON cost_records ("timestamp", cloud_provider, record_type, source_record_hash)
    WHERE cloud_provider = 'aws' AND record_type = 'cur_line_item' AND source_record_hash <> '';

ALTER TABLE cost_records SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'cloud_provider,account_id,service,category',
    timescaledb.compress_orderby = '"timestamp" DESC'
);

SELECT add_compression_policy('cost_records', INTERVAL '7 days', if_not_exists => TRUE);
SELECT add_retention_policy('cost_records', INTERVAL '90 days', if_not_exists => TRUE);

CREATE TABLE IF NOT EXISTS cloud_resources
(
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    resource_name TEXT,
    resource_type TEXT NOT NULL,
    region TEXT,
    scope_id TEXT,
    scope_name TEXT,
    lifecycle_state TEXT,
    availability_domain TEXT,
    time_created TIMESTAMPTZ,
    tags JSONB NOT NULL DEFAULT '{}'::jsonb,
    raw JSONB NOT NULL DEFAULT '{}'::jsonb,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(provider, resource_id)
);

CREATE INDEX IF NOT EXISTS idx_cloud_resources_provider_type
    ON cloud_resources (provider, resource_type);

CREATE INDEX IF NOT EXISTS idx_cloud_resources_provider_region
    ON cloud_resources (provider, region);

CREATE INDEX IF NOT EXISTS idx_cloud_resources_provider_scope
    ON cloud_resources (provider, scope_id, scope_name);

CREATE INDEX IF NOT EXISTS idx_cloud_resources_tags_gin
    ON cloud_resources USING GIN (tags);

CREATE TABLE IF NOT EXISTS cloud_resource_relationships
(
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL,
    source_resource_id TEXT NOT NULL,
    target_resource_id TEXT NOT NULL,
    relationship_type TEXT NOT NULL,
    region TEXT,
    scope_id TEXT,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    raw JSONB NOT NULL DEFAULT '{}'::jsonb,
    UNIQUE(provider, source_resource_id, target_resource_id, relationship_type)
);

CREATE INDEX IF NOT EXISTS idx_cloud_resource_relationships_provider_source
    ON cloud_resource_relationships (provider, source_resource_id);

CREATE INDEX IF NOT EXISTS idx_cloud_resource_relationships_provider_target
    ON cloud_resource_relationships (provider, target_resource_id);

CREATE TABLE IF NOT EXISTS waste_findings
(
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    resource_name TEXT,
    resource_type TEXT NOT NULL,
    region TEXT,
    scope_id TEXT,
    scope_name TEXT,
    service TEXT,
    rule_id TEXT NOT NULL,
    severity TEXT NOT NULL,
    confidence NUMERIC(5,2) NOT NULL,
    estimated_monthly_waste NUMERIC(18,6),
    currency TEXT NOT NULL DEFAULT 'USD',
    summary TEXT NOT NULL,
    recommendation TEXT NOT NULL,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'open',
    detected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    UNIQUE(provider, resource_id, rule_id)
);

CREATE INDEX IF NOT EXISTS idx_waste_findings_provider_status
    ON waste_findings (provider, status, last_seen_at DESC);

CREATE INDEX IF NOT EXISTS idx_waste_findings_provider_rule
    ON waste_findings (provider, rule_id);

CREATE INDEX IF NOT EXISTS idx_waste_findings_provider_region
    ON waste_findings (provider, region);

CREATE INDEX IF NOT EXISTS idx_waste_findings_provider_scope
    ON waste_findings (provider, scope_id, scope_name);

CREATE INDEX IF NOT EXISTS idx_waste_findings_provider_service
    ON waste_findings (provider, service);

CREATE INDEX IF NOT EXISTS idx_waste_findings_provider_resource_type
    ON waste_findings (provider, resource_type);

CREATE INDEX IF NOT EXISTS idx_waste_findings_evidence_gin
    ON waste_findings USING GIN (evidence);
