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

func stringValue(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}
