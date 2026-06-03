package unit

import (
	"os"
	"strings"
	"testing"
)

func TestTimescaleMigrationsContainRequiredPrimitives(t *testing.T) {
	files := []string{
		"../../migrations/001_init.sql",
		"../../migrations/002_processed_report_files.sql",
		"../../migrations/003_processed_reports.sql",
		"../../migrations/004_ingestion_checkpoints_lifecycle.sql",
		"../../migrations/005_cost_records_source_object_index.sql",
		"../../migrations/006_dashboard_analytics.sql",
		"../../migrations/007_dashboard_compartment_summaries.sql",
		"../../migrations/008_rebuild_dashboard_compartment_summaries.sql",
		"../../migrations/009_report_deliveries.sql",
		"../../migrations/012_waste_detection.sql",
		"../../migrations/013_cloud_inventory_runs.sql",
	}

	combined := ""
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		combined += string(b)
	}

	required := []string{
		"CREATE EXTENSION IF NOT EXISTS timescaledb",
		"create_hypertable",
		"cost_summary_daily",
		"cost_summary_weekly",
		"cost_summary_monthly",
		"add_continuous_aggregate_policy",
		"add_retention_policy",
		"processed_reports",
		"ingestion_checkpoints",
		"idx_cost_records_source_object_time",
		"INTERVAL '90 days'",
		"daily_cost_summaries",
		"weekly_cost_summaries",
		"monthly_cost_summaries",
		"cost_anomalies",
		"cost_forecasts",
		"idx_daily_cost_summaries_range_provider",
		"idx_cost_anomalies_range_severity",
		"idx_cost_forecasts_date_provider",
		"oci_compartment_name",
		"report_deliveries",
		"cloud_inventory_runs",
		"active BOOLEAN NOT NULL DEFAULT true",
		"last_seen_run_id TEXT",
		"missing_since TIMESTAMPTZ",
		"inactive_at TIMESTAMPTZ",
	}
	for _, needle := range required {
		if !strings.Contains(combined, needle) {
			t.Fatalf("expected migrations to contain %q", needle)
		}
	}
}

func TestDashboardCompartmentBackfillMigrationRebuildsSummariesFromRawTags(t *testing.T) {
	b, err := os.ReadFile("../../migrations/008_rebuild_dashboard_compartment_summaries.sql")
	if err != nil {
		t.Fatalf("read compartment backfill migration: %v", err)
	}
	migration := string(b)
	for _, required := range []string{
		"DELETE FROM daily_cost_summaries",
		"INSERT INTO daily_cost_summaries",
		"DELETE FROM weekly_cost_summaries",
		"INSERT INTO weekly_cost_summaries",
		"DELETE FROM monthly_cost_summaries",
		"INSERT INTO monthly_cost_summaries",
		"tags->>'oci_compartment_id'",
		"tags->>'oci_compartment_name'",
		"COALESCE(NULLIF(tags->>'oci_compartment_name', ''), tags->>'oci_compartment_id', account_id, '')",
	} {
		if !strings.Contains(migration, required) {
			t.Fatalf("expected compartment backfill migration to contain %q", required)
		}
	}
}

func TestProviderAgnosticCostDimensionsUsesSafeHypertableColumnAdds(t *testing.T) {
	b, err := os.ReadFile("../../migrations/011_provider_agnostic_cost_dimensions.sql")
	if err != nil {
		t.Fatalf("read provider agnostic migration: %v", err)
	}
	migration := string(b)
	indexStart := strings.Index(migration, "CREATE INDEX IF NOT EXISTS idx_cost_records_billing_scope_time")
	if indexStart < 0 {
		t.Fatal("provider agnostic migration is missing cost_records index section")
	}
	costRecordsBlock := migration[:indexStart]
	if strings.Contains(costRecordsBlock, "NOT NULL") {
		t.Fatal("cost_records migration must not add or set NOT NULL constraints")
	}

	for _, forbidden := range []string{
		"ADD COLUMN IF NOT EXISTS billing_scope_type TEXT NOT NULL DEFAULT",
		"ADD COLUMN IF NOT EXISTS billing_scope_id TEXT NOT NULL DEFAULT",
		"ADD COLUMN IF NOT EXISTS billing_scope_name TEXT NOT NULL DEFAULT",
		"ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL DEFAULT",
		"ADD COLUMN IF NOT EXISTS project_name TEXT NOT NULL DEFAULT",
		"ADD COLUMN IF NOT EXISTS project_source TEXT NOT NULL DEFAULT",
		"ADD COLUMN IF NOT EXISTS network_scope_type TEXT NOT NULL DEFAULT",
		"ADD COLUMN IF NOT EXISTS network_scope_id TEXT NOT NULL DEFAULT",
		"ADD COLUMN IF NOT EXISTS network_scope_name TEXT NOT NULL DEFAULT",
		"ADD COLUMN IF NOT EXISTS resource_type TEXT NOT NULL DEFAULT",
		"ADD COLUMN IF NOT EXISTS raw_metadata JSONB NOT NULL DEFAULT",
		"ADD COLUMN IF NOT EXISTS record_type TEXT NOT NULL DEFAULT",
		"ADD COLUMN IF NOT EXISTS source_file_key TEXT NOT NULL DEFAULT",
		"ADD COLUMN IF NOT EXISTS source_file_etag TEXT NOT NULL DEFAULT",
		"ADD COLUMN IF NOT EXISTS source_line_number BIGINT NOT NULL DEFAULT",
		"ADD COLUMN IF NOT EXISTS source_record_hash TEXT NOT NULL DEFAULT",
		"ALTER COLUMN billing_scope_type SET NOT NULL",
		"ALTER COLUMN raw_metadata SET NOT NULL",
		"ALTER COLUMN source_line_number SET NOT NULL",
	} {
		if strings.Contains(costRecordsBlock, forbidden) {
			t.Fatalf("cost_records migration must not use unsafe hypertable pattern %q", forbidden)
		}
	}

	for _, required := range []string{
		"ADD COLUMN IF NOT EXISTS billing_scope_type TEXT",
		"ADD COLUMN IF NOT EXISTS raw_metadata JSONB",
		"ADD COLUMN IF NOT EXISTS source_line_number BIGINT",
		"billing_scope_type = COALESCE(billing_scope_type, '')",
		"raw_metadata = COALESCE(raw_metadata, '{}'::jsonb)",
		"source_line_number = COALESCE(source_line_number, 0)",
		"WHERE billing_scope_type IS NULL",
		"OR raw_metadata IS NULL",
		"OR source_line_number IS NULL",
		"ALTER COLUMN billing_scope_type SET DEFAULT ''",
		"ALTER COLUMN raw_metadata SET DEFAULT '{}'::jsonb",
		"ALTER COLUMN source_line_number SET DEFAULT 0",
	} {
		if !strings.Contains(costRecordsBlock, required) {
			t.Fatalf("cost_records migration must contain safe hypertable step %q", required)
		}
	}
}

func TestProviderAgnosticMigrationKeepsOCIBackfillAndAWSIndexes(t *testing.T) {
	b, err := os.ReadFile("../../migrations/011_provider_agnostic_cost_dimensions.sql")
	if err != nil {
		t.Fatalf("read provider agnostic migration: %v", err)
	}
	migration := string(b)
	for _, required := range []string{
		"billing_scope_type = 'compartment'",
		"billing_scope_id = COALESCE(NULLIF(tags->>'oci_compartment_id', ''), billing_scope_id, '')",
		"billing_scope_name = COALESCE(NULLIF(tags->>'oci_compartment_name', ''), tags->>'oci_compartment_id', billing_scope_name, '')",
		"WHERE cloud_provider = 'oci'",
		"AND COALESCE(billing_scope_type, '') = ''",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_cost_records_aws_idempotency",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_cost_records_aws_cur_record_hash",
		"WHERE cloud_provider = 'aws' AND record_type = 'cur_line_item' AND source_record_hash <> ''",
	} {
		if !strings.Contains(migration, required) {
			t.Fatalf("expected provider agnostic migration to contain %q", required)
		}
	}
}
