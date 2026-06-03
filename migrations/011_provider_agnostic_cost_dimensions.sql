-- torvix:nontransactional
SET timescaledb.max_tuples_decompressed_per_dml_transaction = 0;

ALTER TABLE cost_records
    ADD COLUMN IF NOT EXISTS billing_scope_type TEXT,
    ADD COLUMN IF NOT EXISTS billing_scope_id TEXT,
    ADD COLUMN IF NOT EXISTS billing_scope_name TEXT,
    ADD COLUMN IF NOT EXISTS project_id TEXT,
    ADD COLUMN IF NOT EXISTS project_name TEXT,
    ADD COLUMN IF NOT EXISTS project_source TEXT,
    ADD COLUMN IF NOT EXISTS network_scope_type TEXT,
    ADD COLUMN IF NOT EXISTS network_scope_id TEXT,
    ADD COLUMN IF NOT EXISTS network_scope_name TEXT,
    ADD COLUMN IF NOT EXISTS resource_type TEXT,
    ADD COLUMN IF NOT EXISTS raw_metadata JSONB,
    ADD COLUMN IF NOT EXISTS record_type TEXT,
    ADD COLUMN IF NOT EXISTS source_file_key TEXT,
    ADD COLUMN IF NOT EXISTS source_file_etag TEXT,
    ADD COLUMN IF NOT EXISTS source_line_number BIGINT,
    ADD COLUMN IF NOT EXISTS source_record_hash TEXT;

UPDATE cost_records
SET billing_scope_type = COALESCE(billing_scope_type, ''),
    billing_scope_id = COALESCE(billing_scope_id, ''),
    billing_scope_name = COALESCE(billing_scope_name, ''),
    project_id = COALESCE(project_id, ''),
    project_name = COALESCE(project_name, ''),
    project_source = COALESCE(project_source, ''),
    network_scope_type = COALESCE(network_scope_type, ''),
    network_scope_id = COALESCE(network_scope_id, ''),
    network_scope_name = COALESCE(network_scope_name, ''),
    resource_type = COALESCE(resource_type, ''),
    raw_metadata = COALESCE(raw_metadata, '{}'::jsonb),
    record_type = COALESCE(record_type, ''),
    source_file_key = COALESCE(source_file_key, ''),
    source_file_etag = COALESCE(source_file_etag, ''),
    source_line_number = COALESCE(source_line_number, 0),
    source_record_hash = COALESCE(source_record_hash, '')
WHERE billing_scope_type IS NULL
   OR billing_scope_id IS NULL
   OR billing_scope_name IS NULL
   OR project_id IS NULL
   OR project_name IS NULL
   OR project_source IS NULL
   OR network_scope_type IS NULL
   OR network_scope_id IS NULL
   OR network_scope_name IS NULL
   OR resource_type IS NULL
   OR raw_metadata IS NULL
   OR record_type IS NULL
   OR source_file_key IS NULL
   OR source_file_etag IS NULL
   OR source_line_number IS NULL
   OR source_record_hash IS NULL;

ALTER TABLE cost_records
    ALTER COLUMN billing_scope_type SET DEFAULT '',
    ALTER COLUMN billing_scope_id SET DEFAULT '',
    ALTER COLUMN billing_scope_name SET DEFAULT '',
    ALTER COLUMN project_id SET DEFAULT '',
    ALTER COLUMN project_name SET DEFAULT '',
    ALTER COLUMN project_source SET DEFAULT '',
    ALTER COLUMN network_scope_type SET DEFAULT '',
    ALTER COLUMN network_scope_id SET DEFAULT '',
    ALTER COLUMN network_scope_name SET DEFAULT '',
    ALTER COLUMN resource_type SET DEFAULT '',
    ALTER COLUMN raw_metadata SET DEFAULT '{}'::jsonb,
    ALTER COLUMN record_type SET DEFAULT '',
    ALTER COLUMN source_file_key SET DEFAULT '',
    ALTER COLUMN source_file_etag SET DEFAULT '',
    ALTER COLUMN source_line_number SET DEFAULT 0,
    ALTER COLUMN source_record_hash SET DEFAULT '';

UPDATE cost_records
SET billing_scope_type = 'compartment',
    billing_scope_id = COALESCE(NULLIF(tags->>'oci_compartment_id', ''), billing_scope_id, ''),
    billing_scope_name = COALESCE(NULLIF(tags->>'oci_compartment_name', ''), tags->>'oci_compartment_id', billing_scope_name, '')
WHERE cloud_provider = 'oci'
  AND COALESCE(billing_scope_type, '') = '';

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
