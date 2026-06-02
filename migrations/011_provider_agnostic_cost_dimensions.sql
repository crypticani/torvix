ALTER TABLE cost_records
    ADD COLUMN IF NOT EXISTS billing_scope_type TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS billing_scope_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS billing_scope_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS project_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS project_source TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS network_scope_type TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS network_scope_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS network_scope_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS resource_type TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS raw_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS record_type TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS source_file_key TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS source_file_etag TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS source_line_number BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS source_record_hash TEXT NOT NULL DEFAULT '';

UPDATE cost_records
SET billing_scope_type = 'compartment',
    billing_scope_id = COALESCE(NULLIF(tags->>'oci_compartment_id', ''), billing_scope_id),
    billing_scope_name = COALESCE(NULLIF(tags->>'oci_compartment_name', ''), tags->>'oci_compartment_id', billing_scope_name)
WHERE cloud_provider = 'oci'
  AND billing_scope_type = '';

CREATE INDEX IF NOT EXISTS idx_cost_records_billing_scope_time
    ON cost_records (billing_scope_type, billing_scope_id, "timestamp" DESC);

CREATE INDEX IF NOT EXISTS idx_cost_records_raw_metadata_gin
    ON cost_records USING GIN (raw_metadata jsonb_path_ops);

CREATE UNIQUE INDEX IF NOT EXISTS idx_cost_records_aws_idempotency
    ON cost_records ("timestamp", cloud_provider, region, billing_scope_type, billing_scope_id, service, record_type)
    WHERE cloud_provider = 'aws';

CREATE UNIQUE INDEX IF NOT EXISTS idx_cost_records_aws_cur_record_hash
    ON cost_records ("timestamp", cloud_provider, record_type, source_record_hash)
    WHERE cloud_provider = 'aws' AND record_type = 'cur_line_item' AND source_record_hash <> '';

ALTER TABLE daily_cost_summaries
    ADD COLUMN IF NOT EXISTS billing_scope_type TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS billing_scope_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS billing_scope_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS record_type TEXT NOT NULL DEFAULT '';

ALTER TABLE weekly_cost_summaries
    ADD COLUMN IF NOT EXISTS billing_scope_type TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS billing_scope_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS billing_scope_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS record_type TEXT NOT NULL DEFAULT '';

ALTER TABLE monthly_cost_summaries
    ADD COLUMN IF NOT EXISTS billing_scope_type TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS billing_scope_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS billing_scope_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS record_type TEXT NOT NULL DEFAULT '';

ALTER TABLE daily_cost_summaries DROP CONSTRAINT IF EXISTS daily_cost_summaries_pkey;
ALTER TABLE daily_cost_summaries
    ADD CONSTRAINT daily_cost_summaries_pkey
    PRIMARY KEY (period_start, provider, account_id, compartment_id, compartment_name, record_type, service, category, region, currency);

ALTER TABLE weekly_cost_summaries DROP CONSTRAINT IF EXISTS weekly_cost_summaries_pkey;
ALTER TABLE weekly_cost_summaries
    ADD CONSTRAINT weekly_cost_summaries_pkey
    PRIMARY KEY (period_start, provider, account_id, compartment_id, compartment_name, record_type, service, category, region, currency);

ALTER TABLE monthly_cost_summaries DROP CONSTRAINT IF EXISTS monthly_cost_summaries_pkey;
ALTER TABLE monthly_cost_summaries
    ADD CONSTRAINT monthly_cost_summaries_pkey
    PRIMARY KEY (period_start, provider, account_id, compartment_id, compartment_name, record_type, service, category, region, currency);
