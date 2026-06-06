package unit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDashboardJSONFilesAreValid(t *testing.T) {
	files, err := filepath.Glob("../../dashboards/*.json")
	if err != nil {
		t.Fatalf("glob dashboards: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected at least one dashboard JSON file")
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			b, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read dashboard: %v", err)
			}
			var dashboard map[string]any
			if err := json.Unmarshal(b, &dashboard); err != nil {
				t.Fatalf("dashboard JSON is invalid: %v", err)
			}
			if strings.TrimSpace(stringValue(dashboard["title"])) == "" {
				t.Fatal("dashboard is missing title")
			}
			if strings.TrimSpace(stringValue(dashboard["uid"])) == "" {
				t.Fatal("dashboard is missing uid")
			}
		})
	}
}

func TestNewDashboardsUseTorvixAPIEndpoints(t *testing.T) {
	tests := []struct {
		file     string
		required []string
	}{
		{
			file: "../../dashboards/torvix-waste-dashboard.json",
			required: []string{
				"/api/v1/waste/summary?provider=${provider}",
				"/api/v1/waste/findings?provider=${provider}",
				"/api/v1/waste/rules",
				"TotalOpenFindings",
				"EstimatedMonthlyWaste",
			},
		},
		{
			file: "../../dashboards/torvix-aws-finops-dashboard.json",
			required: []string{
				"/api/v1/dashboard/overview?provider=aws",
				"/api/v1/dashboard/cost-timeseries?provider=aws",
				"/api/v1/dashboard/cost-by-region?provider=aws",
				"/api/v1/dashboard/cost-by-scope?provider=aws",
				"/api/v1/dashboard/cost-by-service?provider=aws",
				"/api/v1/dashboard/drilldown?provider=aws",
				"/api/v1/dashboard/anomalies?provider=aws",
				"/api/v1/dashboard/cost-increases?period=daily&provider=aws",
				"/api/v1/dashboard/cost-increases?period=weekly&provider=aws",
				"/api/v1/dashboard/cost-increases?period=monthly&provider=aws",
				"/api/v1/dashboard/cost-decreases?period=daily&provider=aws",
				"/api/v1/dashboard/cost-decreases?period=weekly&provider=aws",
				"/api/v1/dashboard/cost-decreases?period=monthly&provider=aws",
			},
		},
	}
	for _, tt := range tests {
		t.Run(filepath.Base(tt.file), func(t *testing.T) {
			b, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatalf("read dashboard: %v", err)
			}
			dashboard := string(b)
			for _, required := range tt.required {
				if !strings.Contains(dashboard, required) {
					t.Fatalf("expected dashboard to contain %q", required)
				}
			}
		})
	}
}

func TestAWSDashboardCostChangePanelsUseTwoPanelRows(t *testing.T) {
	b, err := os.ReadFile("../../dashboards/torvix-aws-finops-dashboard.json")
	if err != nil {
		t.Fatalf("read AWS dashboard: %v", err)
	}
	var dashboard struct {
		Panels []struct {
			Title   string `json:"title"`
			GridPos struct {
				H int `json:"h"`
				W int `json:"w"`
				X int `json:"x"`
				Y int `json:"y"`
			} `json:"gridPos"`
		} `json:"panels"`
	}
	if err := json.Unmarshal(b, &dashboard); err != nil {
		t.Fatalf("parse AWS dashboard: %v", err)
	}
	expected := map[string]struct {
		x int
		y int
	}{
		"Daily Cost Increases":   {x: 0, y: 29},
		"Daily Cost Decreases":   {x: 12, y: 29},
		"Weekly Cost Increases":  {x: 0, y: 37},
		"Weekly Cost Decreases":  {x: 12, y: 37},
		"Monthly Cost Increases": {x: 0, y: 45},
		"Monthly Cost Decreases": {x: 12, y: 45},
	}
	for _, panel := range dashboard.Panels {
		want, ok := expected[panel.Title]
		if !ok {
			continue
		}
		if panel.GridPos.X != want.x || panel.GridPos.Y != want.y || panel.GridPos.W != 12 || panel.GridPos.H != 8 {
			t.Fatalf("%s grid position = %+v, want x=%d y=%d w=12 h=8", panel.Title, panel.GridPos, want.x, want.y)
		}
		delete(expected, panel.Title)
	}
	if len(expected) != 0 {
		t.Fatalf("missing AWS cost-change panels: %+v", expected)
	}
}

func TestAnomalyTablesUseReadableEvidenceColumns(t *testing.T) {
	tests := []struct {
		file       string
		panelTitle string
		scopeLabel string
	}{
		{
			file:       "../../dashboards/torvix-aws-finops-dashboard.json",
			panelTitle: "Daily Cost Anomalies vs 7-Day Average",
			scopeLabel: "Account",
		},
		{
			file:       "../../dashboards/torvix-oci-finops-dashboard.json",
			panelTitle: "Daily Cost Anomalies vs 7-Day Average",
			scopeLabel: "Tenancy",
		},
	}

	for _, tt := range tests {
		t.Run(filepath.Base(tt.file), func(t *testing.T) {
			b, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatalf("read dashboard: %v", err)
			}
			var dashboard struct {
				Panels []struct {
					Title       string `json:"title"`
					Description string `json:"description"`
					GridPos     struct {
						H int `json:"h"`
						W int `json:"w"`
						X int `json:"x"`
						Y int `json:"y"`
					} `json:"gridPos"`
					Targets []struct {
						Columns []struct {
							Selector string `json:"selector"`
							Text     string `json:"text"`
							Type     string `json:"type"`
						} `json:"columns"`
					} `json:"targets"`
					FieldConfig struct {
						Overrides []struct {
							Matcher struct {
								Options string `json:"options"`
							} `json:"matcher"`
						} `json:"overrides"`
					} `json:"fieldConfig"`
				} `json:"panels"`
			}
			if err := json.Unmarshal(b, &dashboard); err != nil {
				t.Fatalf("parse dashboard: %v", err)
			}

			var found bool
			for _, panel := range dashboard.Panels {
				if panel.Title != tt.panelTitle {
					continue
				}
				found = true
				if panel.GridPos.X != 0 || panel.GridPos.W != 24 || panel.GridPos.H != 8 {
					t.Fatalf("anomaly table must be full-width, got %+v", panel.GridPos)
				}
				for _, expected := range []string{"previous 7 completed days", "30%", "50%", "standard deviations"} {
					if !strings.Contains(panel.Description, expected) {
						t.Fatalf("anomaly description should explain %q, got %q", expected, panel.Description)
					}
				}
				if len(panel.Targets) != 1 {
					t.Fatalf("expected one anomaly target, got %d", len(panel.Targets))
				}
				expectedColumns := []struct {
					selector string
					text     string
					dataType string
				}{
					{selector: "period_start", text: "Date", dataType: "timestamp"},
					{selector: "account_id", text: tt.scopeLabel, dataType: "string"},
					{selector: "service", text: "Service", dataType: "string"},
					{selector: "region", text: "Region", dataType: "string"},
					{selector: "observed_cost", text: "Actual Spend", dataType: "number"},
					{selector: "expected_cost", text: "7-Day Average", dataType: "number"},
					{selector: "absolute_delta", text: "Difference", dataType: "number"},
					{selector: "percentage_delta", text: "Change", dataType: "number"},
					{selector: "severity", text: "Severity", dataType: "string"},
				}
				if len(panel.Targets[0].Columns) != len(expectedColumns) {
					t.Fatalf("anomaly columns = %+v, want %d readable evidence columns", panel.Targets[0].Columns, len(expectedColumns))
				}
				for i, expected := range expectedColumns {
					column := panel.Targets[0].Columns[i]
					if column.Selector != expected.selector || column.Text != expected.text || column.Type != expected.dataType {
						t.Fatalf("column %d = %+v, want selector=%q text=%q type=%q", i, column, expected.selector, expected.text, expected.dataType)
					}
				}
				overrideNames := make(map[string]struct{}, len(panel.FieldConfig.Overrides))
				for _, override := range panel.FieldConfig.Overrides {
					overrideNames[override.Matcher.Options] = struct{}{}
				}
				for _, expected := range []string{"Date", "Actual Spend", "7-Day Average", "Difference", "Change", "Severity"} {
					if _, ok := overrideNames[expected]; !ok {
						t.Fatalf("expected anomaly table field override for %q", expected)
					}
				}
			}
			if !found {
				t.Fatalf("dashboard is missing panel %q", tt.panelTitle)
			}
		})
	}
}

func stringValue(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}
