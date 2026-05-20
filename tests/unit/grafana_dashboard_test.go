package unit

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestGrafanaDashboardUsesCloudPulseDashboardAPIs(t *testing.T) {
	b, err := os.ReadFile("../../dashboards/cloudpulse-overview.json")
	if err != nil {
		t.Fatalf("read dashboard: %v", err)
	}
	var dashboard struct {
		Time struct {
			From string `json:"from"`
		} `json:"time"`
		Panels []struct {
			Datasource struct {
				UID string `json:"uid"`
			} `json:"datasource"`
			Targets []struct {
				URL  string `json:"url"`
				Expr string `json:"expr"`
			} `json:"targets"`
		} `json:"panels"`
	}
	if err := json.Unmarshal(b, &dashboard); err != nil {
		t.Fatalf("parse dashboard JSON: %v", err)
	}
	if dashboard.Time.From != "now-30d" {
		t.Fatalf("expected 30-day default range, got %q", dashboard.Time.From)
	}
	joined := string(b)
	for _, required := range []string{
		"/api/v1/dashboard/overview",
		"/api/v1/dashboard/cost-timeseries",
		"/api/v1/dashboard/cost-by-category",
		"/api/v1/dashboard/cost-by-service",
		"/api/v1/dashboard/cost-by-provider",
		"/api/v1/dashboard/anomalies",
		"/api/v1/dashboard/ingestion-status",
		"cloudpulse_processed_files_total{status=\\\"skipped_old\\\"}",
		"cloudpulse_records_deleted_total",
		"cloudpulse_compressed_chunks_total",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("expected dashboard to contain %q", required)
		}
	}
	if strings.Contains(joined, "\"uid\": \"PostgreSQL\"") || strings.Contains(joined, "/api/v1/grafana/") {
		t.Fatalf("dashboard must not depend on PostgreSQL datasource or legacy grafana raw endpoints")
	}
}
