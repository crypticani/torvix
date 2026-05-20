CREATE TABLE IF NOT EXISTS ingestion_checkpoints
(
    provider TEXT PRIMARY KEY,
    last_successful_ingestion_at TIMESTAMPTZ NOT NULL DEFAULT 'epoch'::timestamptz,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$
BEGIN
    PERFORM remove_retention_policy('cost_records', if_exists => TRUE);
    PERFORM add_retention_policy('cost_records', INTERVAL '90 days', if_not_exists => TRUE);
END $$;

DO $$
BEGIN
    PERFORM remove_compression_policy('cost_records', if_exists => TRUE);
    PERFORM add_compression_policy('cost_records', INTERVAL '7 days', if_not_exists => TRUE);
END $$;
