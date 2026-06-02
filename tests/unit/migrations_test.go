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
