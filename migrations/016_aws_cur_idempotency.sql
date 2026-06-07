DROP INDEX IF EXISTS idx_cost_records_aws_idempotency;
DROP INDEX IF EXISTS idx_cost_records_aws_cur_record_hash;

CREATE UNIQUE INDEX idx_cost_records_aws_idempotency
    ON cost_records ("timestamp", cloud_provider, region, billing_scope_type, billing_scope_id, service, record_type)
    WHERE cloud_provider = 'aws' AND record_type <> 'cur_line_item';

CREATE UNIQUE INDEX idx_cost_records_aws_cur_record_hash
    ON cost_records ("timestamp", cloud_provider, record_type, source_file_key, source_record_hash)
    WHERE cloud_provider = 'aws'
      AND record_type = 'cur_line_item'
      AND source_file_key <> ''
      AND source_record_hash <> '';
