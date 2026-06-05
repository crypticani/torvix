package unit

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestComposeFilesPassThroughAPIBearerToken(t *testing.T) {
	for _, file := range []string{"../../docker-compose.dev.yml", "../../docker-compose.prod.yml"} {
		t.Run(file, func(t *testing.T) {
			b, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read compose file: %v", err)
			}
			if !strings.Contains(string(b), "TORVIX_API_BEARER_TOKEN") {
				t.Fatalf("expected %s to pass through TORVIX_API_BEARER_TOKEN", file)
			}
		})
	}
}

func TestSwaggerDocumentsCurrentVersionAndBearerAuth(t *testing.T) {
	b, err := os.ReadFile("../../docs/swagger.json")
	if err != nil {
		t.Fatalf("read swagger JSON: %v", err)
	}
	var swagger struct {
		Info struct {
			Version string `json:"version"`
		} `json:"info"`
		Paths map[string]map[string]struct {
			Security []map[string][]string `json:"security"`
		} `json:"paths"`
		SecurityDefinitions map[string]struct {
			Type string `json:"type"`
			In   string `json:"in"`
			Name string `json:"name"`
		} `json:"securityDefinitions"`
	}
	if err := json.Unmarshal(b, &swagger); err != nil {
		t.Fatalf("parse swagger JSON: %v", err)
	}
	if swagger.Info.Version != "0.10.0" {
		t.Fatalf("expected swagger version 0.10.0, got %q", swagger.Info.Version)
	}
	bearer, ok := swagger.SecurityDefinitions["BearerAuth"]
	if !ok {
		t.Fatal("expected BearerAuth security definition")
	}
	if bearer.Type != "apiKey" || bearer.In != "header" || bearer.Name != "Authorization" {
		t.Fatalf("unexpected BearerAuth definition: %+v", bearer)
	}
	for _, route := range []struct {
		path   string
		method string
	}{
		{path: "/api/v1/ingest", method: "post"},
		{path: "/api/v1/ingest/status/{job_id}", method: "get"},
		{path: "/api/v1/analytics/summary", method: "get"},
		{path: "/api/v1/reports/daily", method: "get"},
	} {
		operation, ok := swagger.Paths[route.path][route.method]
		if !ok {
			t.Fatalf("expected swagger operation %s %s", route.method, route.path)
		}
		if !hasBearerSecurity(operation.Security) {
			t.Fatalf("expected %s %s to declare BearerAuth", route.method, route.path)
		}
	}
	if health, ok := swagger.Paths["/healthz"]["get"]; ok && hasBearerSecurity(health.Security) {
		t.Fatal("health endpoint must not require BearerAuth")
	}
}

func TestGeneralDocsRankAWSBeforeOCI(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(b)
	}

	readme := read("../../README.md")
	assertBefore(t, readme, "## AWS Ingestion", "## OCI Billing Ingestion")
	assertBefore(t, readme, "AWS CUR/Data Export ingestion", "OCI billing export ingestion")
	if !strings.Contains(readme, "General provider documentation is ordered by common cloud adoption: AWS first, then OCI.") {
		t.Fatal("README should document AWS-first ordering for general provider docs")
	}

	deployment := read("../../docs/deployment.md")
	assertBefore(t, deployment, "dashboards/torvix-aws-finops-dashboard.json", "dashboards/torvix-oci-finops-dashboard.json")
	assertBefore(t, deployment, "The AWS dashboard production API endpoints are:", "The OCI dashboard production API endpoints are:")
	assertBefore(t, deployment, "The AWS dashboard uses Region -> Account/Scope -> Service", "The OCI dashboard uses Region -> Compartment -> Service")

	agents := read("../../AGENTS.md")
	if !strings.Contains(agents, "AWS and OCI are first-class providers.") {
		t.Fatal("AGENTS should describe AWS and OCI as first-class providers")
	}
	assertBefore(t, agents, "with AWS before OCI", "unless the page or section is explicitly provider-specific")
	assertBefore(t, agents, "AWS ingestion must default", "OCI ingestion must use")
}

func hasBearerSecurity(security []map[string][]string) bool {
	for _, entry := range security {
		if _, ok := entry["BearerAuth"]; ok {
			return true
		}
	}
	return false
}

func assertBefore(t *testing.T, text, first, second string) {
	t.Helper()
	firstIndex := strings.Index(text, first)
	if firstIndex < 0 {
		t.Fatalf("expected to find %q", first)
	}
	secondIndex := strings.Index(text, second)
	if secondIndex < 0 {
		t.Fatalf("expected to find %q", second)
	}
	if firstIndex >= secondIndex {
		t.Fatalf("expected %q to appear before %q", first, second)
	}
}
