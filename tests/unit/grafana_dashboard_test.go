package unit

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestGrafanaDashboardUsesCloudPulseDashboardAPIs(t *testing.T) {
	b, err := os.ReadFile("../../dashboards/cloudpulse-oci-finops-dashboard.json")
	if err != nil {
		t.Fatalf("read dashboard: %v", err)
	}
	var dashboard struct {
		Refresh string `json:"refresh"`
		Version int    `json:"version"`
		Time    struct {
			From string `json:"from"`
		} `json:"time"`
		Templating struct {
			List []struct {
				Name          string `json:"name"`
				QueryType     string `json:"queryType"`
				Refresh       int    `json:"refresh"`
				Query         struct {
					RefID         string `json:"refId"`
					QueryType     string `json:"queryType"`
					InfinityQuery struct {
						URL          string `json:"url"`
						Parser       string `json:"parser"`
						RootSelector string `json:"root_selector"`
						Columns      []struct {
							Text string `json:"text"`
						} `json:"columns"`
					} `json:"infinityQuery"`
				} `json:"query"`
				InfinityQuery struct {
					URL          string `json:"url"`
					Parser       string `json:"parser"`
					RootSelector string `json:"root_selector"`
					Columns      []struct {
						Text string `json:"text"`
					} `json:"columns"`
				} `json:"infinityQuery"`
				Targets []struct {
					RefID         string `json:"refId"`
					QueryType     string `json:"queryType"`
					InfinityQuery struct {
						URL          string `json:"url"`
						Parser       string `json:"parser"`
						RootSelector string `json:"root_selector"`
						Columns      []struct {
							Text string `json:"text"`
						} `json:"columns"`
					} `json:"infinityQuery"`
				} `json:"targets"`
			} `json:"list"`
		} `json:"templating"`
		Panels []struct {
			Datasource struct {
				UID string `json:"uid"`
			} `json:"datasource"`
			Targets []struct {
				URL            string `json:"url"`
				Expr           string `json:"expr"`
				Parser         string `json:"parser"`
				RootSelector   string `json:"root_selector"`
				RootIsNotArray bool   `json:"root_is_not_array"`
			} `json:"targets"`
		} `json:"panels"`
	}
	if err := json.Unmarshal(b, &dashboard); err != nil {
		t.Fatalf("parse dashboard JSON: %v", err)
	}
	if dashboard.Time.From != "now-30d" {
		t.Fatalf("expected 30-day default range, got %q", dashboard.Time.From)
	}
	if dashboard.Refresh != "1m" {
		t.Fatalf("expected dashboard to retry transient initial empty results every minute, got refresh %q", dashboard.Refresh)
	}
	if dashboard.Version < 16 {
		t.Fatalf("expected dashboard version to be bumped for Grafana provisioning reloads, got %d", dashboard.Version)
	}
	joined := string(b)
	for _, required := range []string{
		"/api/v1/dashboard/overview",
		"/api/v1/dashboard/cost-timeseries",
		"/api/v1/dashboard/cost-by-service",
		"/api/v1/dashboard/cost-by-compartment",
		"/api/v1/dashboard/cost-by-region",
		"/api/v1/dashboard/oci-cost-drivers",
		"/api/v1/dashboard/cost-increases",
		"/api/v1/dashboard/cost-decreases",
		"/api/v1/dashboard/anomalies",
		"/api/v1/dashboard/ingestion-status",
		"\"name\": \"region\"",
		"\"name\": \"compartment\"",
		"\"name\": \"service\"",
		"Top OCI Cost Drivers",
		"Daily Cost Decreases",
		"Weekly Cost Decreases",
		"Monthly Cost Decreases",
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
	if !strings.Contains(joined, "from=${__from:date:iso}") || !strings.Contains(joined, "to=${__to:date:iso}") {
		t.Fatalf("dashboard API panels must pass the selected Grafana time range to CloudPulse APIs")
	}
	if strings.Contains(joined, "currencyUSD") {
		t.Fatalf("dashboard must not hardcode USD currency units for OCI cost panels")
	}
	for _, forbidden := range []string{"Account", "Project", "Subscription", "Resource Group"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("OCI dashboard must not mention non-OCI terminology %q", forbidden)
		}
	}
	if !strings.Contains(joined, "region=${region}") ||
		!strings.Contains(joined, "compartment=${compartment}") ||
		!strings.Contains(joined, "service=${service}") {
		t.Fatalf("dashboard drill-down panels must pass region, compartment, and service variables to CloudPulse APIs")
	}
	if len(dashboard.Templating.List) != 3 {
		t.Fatalf("expected region, compartment, and service variables, got %d", len(dashboard.Templating.List))
	}
	for _, variable := range dashboard.Templating.List {
		if variable.QueryType != "infinity" {
			t.Fatalf("variable %q must use Infinity standard variable mode, got queryType %q", variable.Name, variable.QueryType)
		}
		if variable.Refresh != 1 {
			t.Fatalf("variable %q must refresh on dashboard load, got refresh mode %d", variable.Name, variable.Refresh)
		}
		if variable.InfinityQuery.Parser != "backend" || variable.InfinityQuery.RootSelector != "" || variable.InfinityQuery.URL == "" {
			t.Fatalf("variable %q has incomplete Infinity query configuration: %+v", variable.Name, variable.InfinityQuery)
		}
		if !strings.Contains(variable.InfinityQuery.URL, "/api/v1/dashboard/filter-options") {
			t.Fatalf("variable %q must use the unbounded filter-options API, got %q", variable.Name, variable.InfinityQuery.URL)
		}
		if !strings.Contains(variable.InfinityQuery.URL, "format=values") {
			t.Fatalf("variable %q must request single-column values for Grafana, got %q", variable.Name, variable.InfinityQuery.URL)
		}
		if len(variable.InfinityQuery.Columns) != 0 {
			t.Fatalf("variable %q must use Grafana single-column variable mode, got %+v", variable.Name, variable.InfinityQuery.Columns)
		}
		if variable.Query.RefID != "variable" || variable.Query.QueryType != "infinity" {
			t.Fatalf("variable %q must wrap query as an Infinity variable target, got %+v", variable.Name, variable.Query)
		}
		if variable.Query.InfinityQuery.URL != variable.InfinityQuery.URL ||
			variable.Query.InfinityQuery.Parser != variable.InfinityQuery.Parser ||
			variable.Query.InfinityQuery.RootSelector != variable.InfinityQuery.RootSelector ||
			len(variable.Query.InfinityQuery.Columns) != 0 {
			t.Fatalf("variable %q query wrapper must match the Infinity query, got %+v", variable.Name, variable.Query.InfinityQuery)
		}
		if len(variable.Targets) != 1 || variable.Targets[0].RefID != "variable" || variable.Targets[0].QueryType != "infinity" {
			t.Fatalf("variable %q must define a Grafana 13 Infinity variable target, got %+v", variable.Name, variable.Targets)
		}
		targetQuery := variable.Targets[0].InfinityQuery
		if targetQuery.URL != variable.InfinityQuery.URL ||
			targetQuery.Parser != variable.InfinityQuery.Parser ||
			targetQuery.RootSelector != variable.InfinityQuery.RootSelector ||
			len(targetQuery.Columns) != 0 {
			t.Fatalf("variable %q target query must match the Infinity query, got %+v", variable.Name, targetQuery)
		}
	}
	for _, panel := range dashboard.Panels {
		if panel.Datasource.UID != "CloudPulseAPI" {
			continue
		}
		for _, target := range panel.Targets {
			if target.Parser != "backend" {
				t.Fatalf("CloudPulse API target %q must use Infinity backend parser, got %q", target.URL, target.Parser)
			}
			if strings.HasPrefix(target.URL, "/api/v1/dashboard/overview") && target.RootSelector == "data" && !target.RootIsNotArray {
				t.Fatalf("overview target must mark root data object as not an array for Infinity")
			}
		}
	}
}
