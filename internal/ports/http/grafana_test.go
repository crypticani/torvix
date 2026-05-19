package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/crypticani/cloudpulse/internal/core/analytics"
	"github.com/crypticani/cloudpulse/internal/domain"
)

type grafanaRepo struct{}

func (g *grafanaRepo) AggregateCosts(context.Context, time.Time, time.Time, string) ([]domain.AggregatedCost, error) {
	return []domain.AggregatedCost{
		{WindowStart: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), Provider: domain.ProviderOCI, Service: "Compute", TotalCost: 12.5},
		{WindowStart: time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC), Provider: domain.ProviderOCI, Service: "Object Storage", TotalCost: 7.5},
	}, nil
}

func (g *grafanaRepo) DetectAnomalies(context.Context, time.Time, time.Time) ([]domain.Anomaly, error) {
	return []domain.Anomaly{{Date: time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC), Provider: domain.ProviderOCI, Service: "Compute", Actual: 12.5, Baseline: 8, ZScore: 2.1, PercentDeviation: 56.25, Severity: "high"}}, nil
}

func TestGrafanaEndpointsRequireBearerWhenConfigured(t *testing.T) {
	handler := NewWithOptions(nil, analytics.New(&grafanaRepo{}), nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{
		LookbackDays:       30,
		GrafanaAuthEnabled: true,
		GrafanaAuthToken:   "secret",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/grafana/stat/summary?from=2026-05-01&to=2026-05-03", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized without token, got %d", rr.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/grafana/stat/summary?from=2026-05-01&to=2026-05-03", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected ok with token, got %d", rr.Code)
	}
	var got grafanaSummaryStat
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if got.TotalCost != 20 {
		t.Fatalf("expected total cost 20, got %f", got.TotalCost)
	}
	if got.AnomalyCount != 1 {
		t.Fatalf("expected 1 anomaly, got %d", got.AnomalyCount)
	}
}

func TestGrafanaCostTimeseriesShape(t *testing.T) {
	handler := NewWithOptions(nil, analytics.New(&grafanaRepo{}), nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{LookbackDays: 30})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/grafana/timeseries/cost?from=2026-05-01&to=2026-05-03", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected ok, got %d", rr.Code)
	}
	var got []grafanaCostPoint
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode timeseries: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 points, got %d", len(got))
	}
	if got[0].Metric != "oci/Compute" {
		t.Fatalf("unexpected metric %q", got[0].Metric)
	}
}

func (g *grafanaRepo) StoreIngestedBatch(context.Context, domain.ProcessedReportFile, []domain.CanonicalCostRecord) error {
	return nil
}
func (g *grafanaRepo) StoreCostRecords(context.Context, []domain.CanonicalCostRecord) error {
	return nil
}
func (g *grafanaRepo) DeleteCostRecordsForSource(context.Context, domain.Provider, string) error {
	return nil
}
func (g *grafanaRepo) MarkReportProcessed(context.Context, domain.ProcessedReportFile) error {
	return nil
}
func (g *grafanaRepo) LastIngestionCheckpoint(context.Context, domain.Provider) (time.Time, error) {
	return time.Time{}, nil
}
func (g *grafanaRepo) MarkIngestionCheckpoint(context.Context, domain.Provider, time.Time) error {
	return nil
}
func (g *grafanaRepo) CompareCostVariance(context.Context, string, time.Time, time.Time, time.Time, time.Time) ([]domain.CostVariance, error) {
	return nil, nil
}
func (g *grafanaRepo) ForecastCosts(context.Context, time.Time, time.Time, int) ([]domain.ForecastPoint, error) {
	return nil, nil
}
func (g *grafanaRepo) IsReportProcessed(context.Context, domain.Provider, string, string, string) (bool, error) {
	return false, nil
}
func (g *grafanaRepo) ApplyDataLifecyclePolicies(context.Context, int, int) error { return nil }
func (g *grafanaRepo) RunDataLifecycleMaintenance(context.Context, int, int) (domain.DataLifecycleMaintenance, error) {
	return domain.DataLifecycleMaintenance{}, nil
}
func (g *grafanaRepo) RefreshAggregates(context.Context, time.Time, time.Time) error { return nil }
