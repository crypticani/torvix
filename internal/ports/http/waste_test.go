package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/crypticani/torvix/internal/waste"
)

func TestWasteFindingsPassesFilters(t *testing.T) {
	detector := &fakeWasteDetector{}
	handler := NewWithOptions(nil, nil, nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{Waste: detector})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/waste/findings?provider=oci&region=ap-mumbai-1&resource_type=block_volume&status=open&min_confidence=0.9&limit=5&offset=10", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if detector.lastFilters.Provider != "oci" || detector.lastFilters.Region != "ap-mumbai-1" || detector.lastFilters.ResourceType != "block_volume" || detector.lastFilters.Status != "open" {
		t.Fatalf("filters not passed through: %+v", detector.lastFilters)
	}
	if detector.lastFilters.MinConfidence == nil || *detector.lastFilters.MinConfidence != 0.9 {
		t.Fatalf("expected min confidence 0.9, got %+v", detector.lastFilters.MinConfidence)
	}
	if detector.lastFilters.Limit != 5 || detector.lastFilters.Offset != 10 {
		t.Fatalf("expected limit/offset 5/10, got %d/%d", detector.lastFilters.Limit, detector.lastFilters.Offset)
	}
}

func TestWasteSummaryReturnsEmptyWhenDetectorMissing(t *testing.T) {
	handler := NewWithOptions(nil, nil, nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/waste/summary", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var summary waste.Summary
	if err := json.NewDecoder(rec.Body).Decode(&summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summary.TotalOpenFindings != 0 || len(summary.TopFindings) != 0 {
		t.Fatalf("expected empty summary, got %+v", summary)
	}
}

func TestWasteFindingStatusPatch(t *testing.T) {
	detector := &fakeWasteDetector{finding: waste.Finding{ID: 42, Status: waste.StatusIgnored}}
	handler := NewWithOptions(nil, nil, nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{Waste: detector})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/waste/findings/42/status", bytes.NewBufferString(`{"status":"ignored"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if detector.updatedStatus != waste.StatusIgnored {
		t.Fatalf("expected ignored update, got %s", detector.updatedStatus)
	}
}

func TestWasteEndpointsRequireBearerWhenConfigured(t *testing.T) {
	detector := &fakeWasteDetector{finding: waste.Finding{ID: 42, Status: waste.StatusOpen}}
	handler := NewWithOptions(nil, nil, nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{
		Waste:              detector,
		GrafanaAuthEnabled: true,
		GrafanaAuthToken:   "secret",
	})

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "list", method: http.MethodGet, path: "/api/v1/waste/findings"},
		{name: "detail", method: http.MethodGet, path: "/api/v1/waste/findings/42"},
		{name: "status", method: http.MethodPatch, path: "/api/v1/waste/findings/42/status", body: `{"status":"ignored"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected unauthorized without token, got %d", rec.Code)
			}

			req = httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("Authorization", "Bearer secret")
			rec = httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected ok with token, got %d", rec.Code)
			}
		})
	}
}

type fakeWasteDetector struct {
	lastFilters   waste.FindingFilters
	finding       waste.Finding
	updatedStatus string
}

func (f *fakeWasteDetector) Run(context.Context) (waste.DetectionResult, error) {
	return waste.DetectionResult{}, nil
}
func (f *fakeWasteDetector) Rules() []waste.RuleInfo { return []waste.RuleInfo{} }
func (f *fakeWasteDetector) ListFindings(_ context.Context, filters waste.FindingFilters) ([]waste.Finding, error) {
	f.lastFilters = filters
	return []waste.Finding{}, nil
}
func (f *fakeWasteDetector) GetFinding(context.Context, int64) (waste.Finding, error) {
	return f.finding, nil
}
func (f *fakeWasteDetector) UpdateFindingStatus(_ context.Context, _ int64, status string) (waste.Finding, error) {
	f.updatedStatus = status
	f.finding.Status = status
	return f.finding, nil
}
func (f *fakeWasteDetector) Summary(_ context.Context, filters waste.FindingFilters) (waste.Summary, error) {
	f.lastFilters = filters
	return waste.Summary{TopFindings: []waste.Finding{}}, nil
}
