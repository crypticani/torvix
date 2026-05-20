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
	"github.com/crypticani/cloudpulse/internal/core/collect"
	"github.com/crypticani/cloudpulse/internal/domain"
)

type grafanaRepo struct{}

func (g *grafanaRepo) DashboardOverview(context.Context, time.Time, time.Time, time.Time, time.Time) (domain.DashboardOverview, error) {
	return domain.DashboardOverview{
		Current30DaySpend:  20,
		Previous30DaySpend: 10,
		PercentageChange:   100,
		AnomalyCount:       1,
		LatestIngestionAt:  time.Date(2026, 5, 3, 4, 0, 0, 0, time.UTC),
	}, nil
}

func (g *grafanaRepo) DashboardCostSummaries(context.Context, string, time.Time, time.Time) ([]domain.DashboardCostSummary, error) {
	return []domain.DashboardCostSummary{
		{PeriodStart: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), PeriodEnd: time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC), Provider: domain.ProviderOCI, AccountID: "tenancy-a", CompartmentID: "ocid1.compartment.oc1..app", CompartmentName: "app-prod", Service: "Compute", Category: "compute", Region: "us-ashburn-1", TotalCost: 12.5},
		{PeriodStart: time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC), PeriodEnd: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC), Provider: domain.ProviderOCI, AccountID: "tenancy-a", CompartmentID: "ocid1.compartment.oc1..data", CompartmentName: "data-prod", Service: "Object Storage", Category: "storage", Region: "ap-mumbai-1", TotalCost: 7.5},
	}, nil
}

func (g *grafanaRepo) DashboardAnomalies(context.Context, time.Time, time.Time, string) ([]domain.DashboardAnomaly, error) {
	return []domain.DashboardAnomaly{{
		DetectedAt:      time.Date(2026, 5, 2, 6, 0, 0, 0, time.UTC),
		PeriodStart:     time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC),
		Provider:        domain.ProviderOCI,
		Service:         "Compute",
		ObservedCost:    12.5,
		ExpectedCost:    8,
		AbsoluteDelta:   4.5,
		PercentageDelta: 56.25,
		Severity:        "high",
		DetectionMethod: "trailing_7_day_baseline",
		Explanation:     "OCI Compute daily spend was 56% above its trailing baseline: observed 12.50, expected 8.00.",
	}}, nil
}

func (g *grafanaRepo) LatestIngestionStatus(context.Context) (domain.IngestionStatusSummary, error) {
	return domain.IngestionStatusSummary{
		LatestIngestionAt: time.Date(2026, 5, 3, 4, 0, 0, 0, time.UTC),
		Providers: []domain.ProviderIngestionStatus{
			{Provider: domain.ProviderOCI, LastSuccessfulIngestionAt: time.Date(2026, 5, 3, 4, 0, 0, 0, time.UTC), FilesProcessed: 2, RecordsProcessed: 20},
		},
	}, nil
}

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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/overview?from=2026-05-01&to=2026-05-03", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized without token, got %d", rr.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/overview?from=2026-05-01&to=2026-05-03", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected ok with token, got %d", rr.Code)
	}
	var got dashboardOverviewResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if got.Data.Current30DaySpend != 20 {
		t.Fatalf("expected current spend 20, got %f", got.Data.Current30DaySpend)
	}
	if got.Data.AnomalyCount != 1 {
		t.Fatalf("expected 1 anomaly, got %d", got.Data.AnomalyCount)
	}
	if got.Meta.RetentionDays != 90 {
		t.Fatalf("expected retention metadata 90, got %d", got.Meta.RetentionDays)
	}
}

func TestDashboardCostTimeseriesShape(t *testing.T) {
	handler := NewWithOptions(nil, analytics.New(&grafanaRepo{}), nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{LookbackDays: 30})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/cost-timeseries?from=2026-05-01&to=2026-05-03", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected ok, got %d", rr.Code)
	}
	var got dashboardTimeseriesResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode timeseries: %v", err)
	}
	if len(got.Data) != 2 {
		t.Fatalf("expected 2 points, got %d", len(got.Data))
	}
	if got.Data[0].Metric != "oci/tenancy-a/total" {
		t.Fatalf("unexpected metric %q", got.Data[0].Metric)
	}
	if got.Meta.Source != "precomputed" {
		t.Fatalf("expected precomputed source metadata, got %q", got.Meta.Source)
	}
}

func TestDashboardOverviewCanBeProviderScoped(t *testing.T) {
	handler := NewWithOptions(nil, analytics.New(&grafanaRepo{}), nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{LookbackDays: 30})

	got, err := handler.dashboardOverviewForProvider(context.Background(), domain.ProviderOCI, time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("provider overview: %v", err)
	}
	if got.Current30DaySpend != 20 {
		t.Fatalf("expected provider current spend 20, got %f", got.Current30DaySpend)
	}
	if got.AnomalyCount != 1 {
		t.Fatalf("expected provider anomaly count 1, got %d", got.AnomalyCount)
	}
}

func TestDashboardCostTimeseriesAcceptsRFC3339DateRange(t *testing.T) {
	handler := NewWithOptions(nil, analytics.New(&grafanaRepo{}), nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{LookbackDays: 30})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/cost-timeseries?from=2026-05-01T00:00:00Z&to=2026-05-03T00:00:00Z", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected ok, got %d", rr.Code)
	}
	var got dashboardTimeseriesResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode timeseries: %v", err)
	}
	if len(got.Data) != 2 {
		t.Fatalf("expected RFC3339 dashboard range to return data, got %d points with metadata %+v", len(got.Data), got.Meta)
	}
	if got.Meta.Message != "" {
		t.Fatalf("expected no metadata warning for RFC3339 range, got %q", got.Meta.Message)
	}
}

func TestDashboardBreakdownSupportsOCICompartmentAndRegion(t *testing.T) {
	handler := NewWithOptions(nil, analytics.New(&grafanaRepo{}), nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{LookbackDays: 30})

	for _, tc := range []struct {
		path    string
		want    string
		wantSum float64
	}{
		{path: "/api/v1/dashboard/cost-by-compartment?from=2026-05-01&to=2026-05-03", want: "app-prod", wantSum: 12.5},
		{path: "/api/v1/dashboard/cost-by-region?from=2026-05-01&to=2026-05-03", want: "us-ashburn-1", wantSum: 12.5},
	} {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("expected ok, got %d", rr.Code)
			}
			var got dashboardBreakdownResponse
			if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
				t.Fatalf("decode breakdown: %v", err)
			}
			if len(got.Data) == 0 {
				t.Fatalf("expected breakdown data for %s", tc.path)
			}
			if got.Data[0].Name != tc.want || got.Data[0].TotalCost != tc.wantSum {
				t.Fatalf("unexpected top breakdown row: %+v", got.Data[0])
			}
		})
	}
}

func TestDashboardAnomaliesReturnsEmptyArrayShape(t *testing.T) {
	handler := NewWithOptions(nil, analytics.New(&emptyDashboardRepo{}), nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{LookbackDays: 30})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/anomalies?from=2026-05-01&to=2026-05-03", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected ok, got %d", rr.Code)
	}
	var got dashboardAnomaliesResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode anomalies: %v", err)
	}
	if got.Data == nil {
		t.Fatal("expected empty anomaly list, got null")
	}
	if len(got.Data) != 0 {
		t.Fatalf("expected no anomalies, got %d", len(got.Data))
	}
	if got.Meta.Message != "no anomalies detected" {
		t.Fatalf("expected no anomalies metadata, got %q", got.Meta.Message)
	}
}

func TestDashboardRangeOutsideRetentionReturnsMetadataAndEmptyData(t *testing.T) {
	handler := NewWithOptions(nil, analytics.New(&grafanaRepo{}), nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{
		LookbackDays:  30,
		RetentionDays: 90,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/cost-timeseries?from=2025-01-01&to=2025-01-31", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected ok, got %d", rr.Code)
	}
	var got dashboardTimeseriesResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode timeseries: %v", err)
	}
	if got.Data == nil {
		t.Fatal("expected empty data list, got null")
	}
	if len(got.Data) != 0 {
		t.Fatalf("expected no data outside retention, got %d", len(got.Data))
	}
	if got.Meta.RetentionDays != 90 || got.Meta.Message == "" {
		t.Fatalf("expected retention metadata and message, got %+v", got.Meta)
	}
}

func TestIngestionResponseIncludesRecordLookbackCounters(t *testing.T) {
	results := ingestionResponses([]collect.ProviderResult{{
		Provider:              "oci",
		RecordsParsed:         5911,
		RecordsWithinLookback: 0,
		RecordsSkippedOld:     5911,
		RecordsInserted:       0,
	}})
	if len(results) != 1 {
		t.Fatalf("expected one response, got %d", len(results))
	}
	got := results[0]
	if got.RecordsParsed != 5911 || got.RecordsWithinLookback != 0 || got.RecordsSkippedOld != 5911 || got.RecordsInserted != 0 {
		t.Fatalf("unexpected ingestion response counters: %+v", got)
	}
}

type emptyDashboardRepo struct {
	grafanaRepo
}

func (g *emptyDashboardRepo) DashboardAnomalies(context.Context, time.Time, time.Time, string) ([]domain.DashboardAnomaly, error) {
	return []domain.DashboardAnomaly{}, nil
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
func (g *grafanaRepo) RefreshDashboardAnalytics(context.Context, time.Time, time.Time, int) error {
	return nil
}
