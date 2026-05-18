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
	}
	for _, needle := range required {
		if !strings.Contains(combined, needle) {
			t.Fatalf("expected migrations to contain %q", needle)
		}
	}
}
