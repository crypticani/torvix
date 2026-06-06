package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/crypticani/torvix/internal/core/analytics"
	"github.com/crypticani/torvix/internal/core/collect"
	"github.com/crypticani/torvix/internal/domain"
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
		{PeriodStart: time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC), PeriodEnd: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC), Provider: domain.ProviderOCI, AccountID: "tenancy-a", CompartmentID: "ocid1.compartment.oc1..app", CompartmentName: "app-prod", Service: "Database", Category: "database", Region: "us-ashburn-1", TotalCost: 5},
		{PeriodStart: time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC), PeriodEnd: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC), Provider: domain.ProviderOCI, AccountID: "tenancy-a", Region: "us-ashburn-1", TotalCost: 2},
		{PeriodStart: time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC), PeriodEnd: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC), Provider: domain.ProviderAWS, AccountID: "123456789012", BillingScopeType: "linked_account", BillingScopeID: "123456789012", BillingScopeName: "prod-account", RecordType: "linked_account_service", Service: "EC2", Category: "compute", Region: "us-east-1", TotalCost: 100},
		{PeriodStart: time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC), PeriodEnd: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC), Provider: domain.ProviderAWS, AccountID: "all", BillingScopeType: "linked_account", BillingScopeID: "all", RecordType: "region_service", Service: "EC2", Category: "compute", Region: "us-east-1", TotalCost: 100},
		{PeriodStart: time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC), PeriodEnd: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC), Provider: domain.ProviderAWS, AccountID: "123456789012", BillingScopeType: "linked_account", BillingScopeID: "123456789012", BillingScopeName: "prod-account", RecordType: "cur_line_item", Service: "EC2", Category: "compute", Region: "us-east-1", TotalCost: 80},
	}, nil
}

func (g *grafanaRepo) DashboardAnomalies(context.Context, time.Time, time.Time, string) ([]domain.DashboardAnomaly, error) {
	return []domain.DashboardAnomaly{
		{
			DetectedAt:      time.Date(2026, 5, 2, 6, 0, 0, 0, time.UTC),
			PeriodStart:     time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC),
			Provider:        domain.ProviderOCI,
			AccountID:       "tenancy-a",
			CompartmentID:   "ocid1.compartment.oc1..app",
			CompartmentName: "app-prod",
			Service:         "Compute",
			Region:          "us-ashburn-1",
			Currency:        "USD",
			ObservedCost:    12.5,
			ExpectedCost:    8,
			AbsoluteDelta:   4.5,
			PercentageDelta: 56.25,
			Direction:       "increase",
			Severity:        "high",
			DetectionMethod: "trailing_7_day_baseline",
			Explanation:     "OCI Compute daily spend was 56% above its trailing baseline: observed 12.50, expected 8.00.",
		},
		{
			DetectedAt:      time.Date(2026, 5, 2, 6, 0, 0, 0, time.UTC),
			PeriodStart:     time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC),
			Provider:        domain.ProviderOCI,
			AccountID:       "tenancy-a",
			CompartmentID:   "ocid1.compartment.oc1..data",
			CompartmentName: "data-prod",
			Service:         "Object Storage",
			Region:          "ap-mumbai-1",
			Currency:        "USD",
			ObservedCost:    4,
			ExpectedCost:    10,
			AbsoluteDelta:   -6,
			PercentageDelta: -60,
			Direction:       "decrease",
			Severity:        "high",
			DetectionMethod: "trailing_7_day_baseline",
			Explanation:     "OCI Object Storage daily spend was 60% below its trailing baseline: observed 4.00, expected 10.00.",
		},
	}, nil
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
		LookbackDays:   30,
		APIAuthEnabled: true,
		APIAuthToken:   "secret",
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/cost-timeseries?provider=oci&from=2026-05-01&to=2026-05-03", nil)
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

func TestDashboardCostTimeseriesDoesNotDoubleCountAWSQueryViews(t *testing.T) {
	handler := NewWithOptions(nil, analytics.New(&grafanaRepo{}), nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{LookbackDays: 30})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/cost-timeseries?provider=aws&from=2026-05-01&to=2026-05-03", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected ok, got %d", rr.Code)
	}
	var got dashboardTimeseriesResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode timeseries: %v", err)
	}
	if len(got.Data) != 1 {
		t.Fatalf("expected one canonical AWS point, got %d: %+v", len(got.Data), got.Data)
	}
	if got.Data[0].Value != 80 {
		t.Fatalf("expected AWS spend 80 from CUR without Cost Explorer duplicates, got %+v", got.Data[0])
	}
}

func TestDashboardOverviewCanBeProviderScoped(t *testing.T) {
	handler := NewWithOptions(nil, analytics.New(&grafanaRepo{}), nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{LookbackDays: 30})

	got, err := handler.dashboardOverviewForProvider(context.Background(), domain.ProviderOCI, time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("provider overview: %v", err)
	}
	if got.Current30DaySpend != 27 {
		t.Fatalf("expected provider current spend 27, got %f", got.Current30DaySpend)
	}
	if got.AnomalyCount != 2 {
		t.Fatalf("expected provider anomaly count 2, got %d", got.AnomalyCount)
	}
}

func TestDashboardCostTimeseriesAcceptsRFC3339DateRange(t *testing.T) {
	handler := NewWithOptions(nil, analytics.New(&grafanaRepo{}), nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{LookbackDays: 30})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/cost-timeseries?provider=oci&from=2026-05-01T00:00:00Z&to=2026-05-03T00:00:00Z", nil)
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
		{path: "/api/v1/dashboard/cost-by-compartment?provider=oci&from=2026-05-01&to=2026-05-03", want: "app-prod", wantSum: 17.5},
		{path: "/api/v1/dashboard/cost-by-region?provider=oci&from=2026-05-01&to=2026-05-03", want: "us-ashburn-1", wantSum: 19.5},
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

func TestDashboardBreakdownSupportsAWSScope(t *testing.T) {
	handler := NewWithOptions(nil, analytics.New(&grafanaRepo{}), nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{LookbackDays: 30})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/cost-by-scope?provider=aws&from=2026-05-01&to=2026-05-03", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected ok, got %d", rr.Code)
	}
	var got dashboardBreakdownResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode breakdown: %v", err)
	}
	if len(got.Data) != 1 {
		t.Fatalf("expected one AWS scope row, got %d: %+v", len(got.Data), got.Data)
	}
	if got.Data[0].Name != "prod-account" || got.Data[0].TotalCost != 80 {
		t.Fatalf("unexpected AWS scope row: %+v", got.Data[0])
	}
}

func TestDashboardDrilldownUsesProviderScopeLabel(t *testing.T) {
	handler := NewWithOptions(nil, analytics.New(&grafanaRepo{}), nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{LookbackDays: 30})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/drilldown?provider=aws&from=2026-05-01&to=2026-05-03", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected ok, got %d", rr.Code)
	}
	var got dashboardDrilldownResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode drilldown: %v", err)
	}
	if len(got.Data) != 1 {
		t.Fatalf("expected one AWS drilldown row, got %d: %+v", len(got.Data), got.Data)
	}
	row := got.Data[0]
	if row.Region != "us-east-1" || row.Scope != "prod-account" || row.ScopeLabel != "Linked Account" || row.Service != "EC2" || row.TotalCost != 80 {
		t.Fatalf("unexpected AWS drilldown row: %+v", row)
	}
}

func TestDashboardBreakdownSupportsOCIRegionCompartmentServiceDrilldown(t *testing.T) {
	handler := NewWithOptions(nil, analytics.New(&grafanaRepo{}), nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{LookbackDays: 30})

	for _, tc := range []struct {
		name       string
		path       string
		wantRows   []string
		wantTotals []float64
	}{
		{
			name:       "compartments can be filtered by region",
			path:       "/api/v1/dashboard/cost-by-compartment?provider=oci&region=us-ashburn-1&from=2026-05-01&to=2026-05-03",
			wantRows:   []string{"app-prod", "Unknown"},
			wantTotals: []float64{17.5, 2},
		},
		{
			name:       "services can be filtered by region and compartment",
			path:       "/api/v1/dashboard/cost-by-service?provider=oci&region=us-ashburn-1&compartment=app-prod&from=2026-05-01&to=2026-05-03",
			wantRows:   []string{"Compute", "Database"},
			wantTotals: []float64{12.5, 5},
		},
		{
			name:       "all variable values do not filter",
			path:       "/api/v1/dashboard/cost-by-service?provider=oci&region=All&compartment=All&service=All&from=2026-05-01&to=2026-05-03",
			wantRows:   []string{"Compute", "Object Storage", "Database", "Unknown"},
			wantTotals: []float64{12.5, 7.5, 5, 2},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			if len(got.Data) != len(tc.wantRows) {
				t.Fatalf("expected %d rows, got %d: %+v", len(tc.wantRows), len(got.Data), got.Data)
			}
			for i, want := range tc.wantRows {
				if got.Data[i].Name != want || got.Data[i].TotalCost != tc.wantTotals[i] {
					t.Fatalf("row %d mismatch: got %+v, want %s %.2f", i, got.Data[i], want, tc.wantTotals[i])
				}
			}
		})
	}
}

func TestDashboardOCICostDriversReturnsPercentOfFilteredTotal(t *testing.T) {
	handler := NewWithOptions(nil, analytics.New(&grafanaRepo{}), nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{LookbackDays: 30})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/oci-cost-drivers?region=us-ashburn-1&compartment=app-prod&from=2026-05-01&to=2026-05-03", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected ok, got %d", rr.Code)
	}
	var got dashboardCostDriversResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode cost drivers: %v", err)
	}
	if len(got.Data) != 2 {
		t.Fatalf("expected two cost drivers, got %d: %+v", len(got.Data), got.Data)
	}
	if got.Data[0].Region != "us-ashburn-1" || got.Data[0].Compartment != "app-prod" || got.Data[0].Service != "Compute" {
		t.Fatalf("unexpected top cost driver: %+v", got.Data[0])
	}
	if got.Data[0].TotalCost != 12.5 || got.Data[0].Percentage != 71.42857142857143 {
		t.Fatalf("unexpected top cost driver totals: %+v", got.Data[0])
	}
	if got.Data[1].Service != "Database" || got.Data[1].TotalCost != 5 || got.Data[1].Percentage != 28.57142857142857 {
		t.Fatalf("unexpected second cost driver: %+v", got.Data[1])
	}
}

func TestDashboardFilterOptionsReturnsCompleteUnboundedList(t *testing.T) {
	repo := &variableOptionsRepo{}
	handler := NewWithOptions(nil, analytics.New(repo), nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{LookbackDays: 30, RetentionDays: 90})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/filter-options?dimension=service&provider=oci", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected ok, got %d", rr.Code)
	}
	var got struct {
		Data []struct {
			Text  string `json:"__text"`
			Value string `json:"__value"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode filter options: %v", err)
	}
	if len(got.Data) != 20 {
		t.Fatalf("expected all 20 service options, got %d: %+v", len(got.Data), got.Data)
	}
	if got.Data[0].Text != "Service 01" || got.Data[0].Value != "Service 01" {
		t.Fatalf("unexpected first option: %+v", got.Data[0])
	}
	if got.Data[19].Text != "Service 20" || got.Data[19].Value != "Service 20" {
		t.Fatalf("unexpected last option: %+v", got.Data[19])
	}
	if repo.to.Sub(repo.from) < 85*24*time.Hour {
		t.Fatalf("filter options must query the retention window by default, got %s to %s", repo.from, repo.to)
	}
}

func TestDashboardFilterOptionsCanReturnGrafanaValueArray(t *testing.T) {
	repo := &variableOptionsRepo{}
	handler := NewWithOptions(nil, analytics.New(repo), nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{LookbackDays: 30, RetentionDays: 90})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/filter-options?dimension=service&provider=oci&format=values", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected ok, got %d", rr.Code)
	}
	var got []string
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode filter options: %v", err)
	}
	if len(got) != 20 {
		t.Fatalf("expected all 20 service options, got %d: %+v", len(got), got)
	}
	if got[0] != "Service 01" || got[19] != "Service 20" {
		t.Fatalf("unexpected service options: %+v", got)
	}
}

func TestDashboardCostIncreasesReturnsCompletedWindowIncreases(t *testing.T) {
	repo := &costIncreaseRepo{}
	handler := NewWithOptions(nil, analytics.New(repo), nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{LookbackDays: 30})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/cost-increases?period=daily&provider=oci&limit=2&as_of=2026-05-21", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected ok, got %d", rr.Code)
	}
	if !repo.currentFrom.Equal(time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)) ||
		!repo.currentTo.Equal(time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)) ||
		!repo.previousFrom.Equal(time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)) ||
		!repo.previousTo.Equal(time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("daily comparison must use day-1 and day-2 windows, got current %s-%s previous %s-%s", repo.currentFrom, repo.currentTo, repo.previousFrom, repo.previousTo)
	}
	var got dashboardCostIncreasesResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode cost increases: %v", err)
	}
	if len(got.Data) != 2 {
		t.Fatalf("expected top 2 OCI increases, got %d: %+v", len(got.Data), got.Data)
	}
	if !got.Meta.From.Equal(time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)) ||
		!got.Meta.To.Equal(time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected response metadata to describe the evaluated window, got %s-%s", got.Meta.From, got.Meta.To)
	}
	if got.Data[0].CompartmentName != "app-prod" || got.Data[0].Service != "Compute" || got.Data[0].Delta != 60 {
		t.Fatalf("unexpected top increase: %+v", got.Data[0])
	}
	if got.Data[1].CompartmentName != "data-prod" || got.Data[1].Service != "Object Storage" || got.Data[1].Delta != 10 {
		t.Fatalf("unexpected second increase: %+v", got.Data[1])
	}
	for _, row := range got.Data {
		if row.Direction != "increase" || row.Provider != domain.ProviderOCI || row.Delta <= 0 {
			t.Fatalf("unexpected non-increase or wrong provider row: %+v", row)
		}
	}
}

func TestDashboardCostDecreasesReturnsCompletedWindowDecreases(t *testing.T) {
	repo := &costIncreaseRepo{}
	handler := NewWithOptions(nil, analytics.New(repo), nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{LookbackDays: 30})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/cost-decreases?period=daily&provider=oci&limit=2&as_of=2026-05-21", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected ok, got %d", rr.Code)
	}
	var got dashboardCostIncreasesResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode cost decreases: %v", err)
	}
	if len(got.Data) != 1 {
		t.Fatalf("expected one OCI decrease, got %d: %+v", len(got.Data), got.Data)
	}
	if got.Data[0].CompartmentName != "down-prod" || got.Data[0].Service != "Database" || got.Data[0].Delta != -10 {
		t.Fatalf("unexpected top decrease: %+v", got.Data[0])
	}
	for _, row := range got.Data {
		if row.Direction != "decrease" || row.Provider != domain.ProviderOCI || row.Delta >= 0 {
			t.Fatalf("unexpected non-decrease or wrong provider row: %+v", row)
		}
	}
}

func TestDashboardDailyCostIncreasesFallsBackWhenLatestDayIsPartial(t *testing.T) {
	repo := &partialDailyCostIncreaseRepo{}
	handler := NewWithOptions(nil, analytics.New(repo), nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{LookbackDays: 30})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/cost-increases?period=daily&provider=oci&limit=2&as_of=2026-05-27", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected ok, got %d", rr.Code)
	}

	if !repo.comparedCurrentFrom.Equal(time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC)) ||
		!repo.comparedCurrentTo.Equal(time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected fallback to latest usable prior day, got current %s-%s", repo.comparedCurrentFrom, repo.comparedCurrentTo)
	}
	var got dashboardCostIncreasesResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode cost increases: %v", err)
	}
	if len(got.Data) != 1 {
		t.Fatalf("expected one fallback increase, got %d: %+v", len(got.Data), got.Data)
	}
	if got.Data[0].Service != "Compute" || got.Data[0].Delta != 60 {
		t.Fatalf("unexpected fallback increase: %+v", got.Data[0])
	}
	if !got.Meta.From.Equal(time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC)) ||
		!got.Meta.To.Equal(time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected response metadata to describe fallback window, got %s-%s", got.Meta.From, got.Meta.To)
	}
}

func TestDashboardCostIncreaseWindowsEndBeforeToday(t *testing.T) {
	asOf := time.Date(2026, 5, 21, 15, 30, 0, 0, time.UTC)
	tests := []struct {
		period       string
		currentFrom  time.Time
		currentTo    time.Time
		previousFrom time.Time
		previousTo   time.Time
	}{
		{
			period:       "daily",
			currentFrom:  time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC),
			currentTo:    time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC),
			previousFrom: time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC),
			previousTo:   time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC),
		},
		{
			period:       "weekly",
			currentFrom:  time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC),
			currentTo:    time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC),
			previousFrom: time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC),
			previousTo:   time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC),
		},
		{
			period:       "monthly",
			currentFrom:  time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC),
			currentTo:    time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC),
			previousFrom: time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC),
			previousTo:   time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.period, func(t *testing.T) {
			currentFrom, currentTo, previousFrom, previousTo := dashboardIncreaseWindows(tt.period, asOf)
			if !currentFrom.Equal(tt.currentFrom) || !currentTo.Equal(tt.currentTo) ||
				!previousFrom.Equal(tt.previousFrom) || !previousTo.Equal(tt.previousTo) {
				t.Fatalf("windows mismatch: got current %s-%s previous %s-%s, want current %s-%s previous %s-%s", currentFrom, currentTo, previousFrom, previousTo, tt.currentFrom, tt.currentTo, tt.previousFrom, tt.previousTo)
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

func TestDashboardAnomaliesAppliesProviderAndDimensionFilters(t *testing.T) {
	handler := NewWithOptions(nil, analytics.New(&grafanaRepo{}), nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{LookbackDays: 30})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/anomalies?from=2026-05-01&to=2026-05-03&provider=oci&compartment=app-prod&region=us-ashburn-1&service=Compute", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected ok, got %d", rr.Code)
	}
	var got dashboardAnomaliesResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode anomalies: %v", err)
	}
	if len(got.Data) != 1 {
		t.Fatalf("expected one matching anomaly, got %+v", got.Data)
	}
	if got.Data[0].CompartmentName != "app-prod" || got.Data[0].Direction != "increase" {
		t.Fatalf("unexpected filtered anomaly: %+v", got.Data[0])
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
func (g *grafanaRepo) IsReportDelivered(context.Context, domain.ReportDeliveryKey) (bool, error) {
	return false, nil
}
func (g *grafanaRepo) RecordReportDelivery(context.Context, domain.ReportDeliveryKey) error {
	return nil
}
func (g *grafanaRepo) ApplyDataLifecyclePolicies(context.Context, int, int) error { return nil }
func (g *grafanaRepo) RunDataLifecycleMaintenance(context.Context, int, int) (domain.DataLifecycleMaintenance, error) {
	return domain.DataLifecycleMaintenance{}, nil
}
func (g *grafanaRepo) RefreshAggregates(context.Context, time.Time, time.Time) error { return nil }
func (g *grafanaRepo) RefreshDashboardAnalytics(context.Context, time.Time, time.Time, int) error {
	return nil
}

type costIncreaseRepo struct {
	grafanaRepo
	currentFrom  time.Time
	currentTo    time.Time
	previousFrom time.Time
	previousTo   time.Time
}

func (r *costIncreaseRepo) CompareCostVariance(_ context.Context, period string, currentFrom, currentTo, previousFrom, previousTo time.Time) ([]domain.CostVariance, error) {
	r.currentFrom = currentFrom
	r.currentTo = currentTo
	r.previousFrom = previousFrom
	r.previousTo = previousTo
	return []domain.CostVariance{
		{Period: period, Provider: domain.ProviderOCI, AccountID: "tenancy-a", CompartmentName: "app-prod", Service: "Compute", CurrentCost: 100, PreviousCost: 40, Delta: 60, PercentChange: 150, Direction: "increase"},
		{Period: period, Provider: domain.ProviderOCI, AccountID: "tenancy-a", CompartmentName: "data-prod", Service: "Object Storage", CurrentCost: 25, PreviousCost: 15, Delta: 10, PercentChange: 66.67, Direction: "increase"},
		{Period: period, Provider: domain.ProviderOCI, AccountID: "tenancy-a", CompartmentName: "flat-prod", Service: "Network", CurrentCost: 20, PreviousCost: 20, Delta: 0, Direction: "flat"},
		{Period: period, Provider: domain.ProviderOCI, AccountID: "tenancy-a", CompartmentName: "down-prod", Service: "Database", CurrentCost: 5, PreviousCost: 15, Delta: -10, Direction: "decrease"},
		{Period: period, Provider: domain.ProviderAWS, AccountID: "acct-b", CompartmentName: "aws-prod", Service: "EC2", CurrentCost: 500, PreviousCost: 100, Delta: 400, Direction: "increase"},
	}, nil
}

type variableOptionsRepo struct {
	grafanaRepo
	from time.Time
	to   time.Time
}

func (r *variableOptionsRepo) DashboardCostSummaries(_ context.Context, _ string, from time.Time, to time.Time) ([]domain.DashboardCostSummary, error) {
	r.from = from
	r.to = to
	out := make([]domain.DashboardCostSummary, 0, 20)
	for i := 1; i <= 20; i++ {
		out = append(out, domain.DashboardCostSummary{
			Provider:    domain.ProviderOCI,
			Service:     fmt.Sprintf("Service %02d", i),
			TotalCost:   float64(i),
			PeriodStart: time.Date(2026, 5, i, 0, 0, 0, 0, time.UTC),
		})
	}
	return out, nil
}

type partialDailyCostIncreaseRepo struct {
	grafanaRepo
	comparedCurrentFrom time.Time
	comparedCurrentTo   time.Time
}

func (r *partialDailyCostIncreaseRepo) DashboardCostSummaries(context.Context, string, time.Time, time.Time) ([]domain.DashboardCostSummary, error) {
	return []domain.DashboardCostSummary{
		{PeriodStart: time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC), PeriodEnd: time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC), Provider: domain.ProviderOCI, TotalCost: 50},
		{PeriodStart: time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC), PeriodEnd: time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC), Provider: domain.ProviderOCI, TotalCost: 100},
		{PeriodStart: time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC), PeriodEnd: time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC), Provider: domain.ProviderOCI, TotalCost: 2},
	}, nil
}

func (r *partialDailyCostIncreaseRepo) CompareCostVariance(_ context.Context, period string, currentFrom, currentTo, previousFrom, previousTo time.Time) ([]domain.CostVariance, error) {
	r.comparedCurrentFrom = currentFrom
	r.comparedCurrentTo = currentTo
	if currentFrom.Equal(time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC)) {
		return []domain.CostVariance{
			{Period: period, Provider: domain.ProviderOCI, AccountID: "tenancy-a", CompartmentName: "app-prod", Service: "Compute", CurrentCost: 100, PreviousCost: 40, Delta: 60, PercentChange: 150, Direction: "increase"},
		}, nil
	}
	return []domain.CostVariance{}, nil
}
