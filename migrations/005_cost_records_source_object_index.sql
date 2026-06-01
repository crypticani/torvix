-- torvix:nontransactional
CREATE INDEX IF NOT EXISTS idx_cost_records_source_object_time
    ON cost_records (cloud_provider, source_object, "timestamp" DESC)
    WITH (timescaledb.transaction_per_chunk);
