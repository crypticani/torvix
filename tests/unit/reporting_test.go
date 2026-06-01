package unit

import (
	"context"
	"testing"
	"time"

	"github.com/crypticani/torvix/internal/core/analytics"
	"github.com/crypticani/torvix/internal/core/forecasting"
	"github.com/crypticani/torvix/internal/core/reporting"
	"github.com/crypticani/torvix/internal/domain"
)

type mockReportingRepo struct{}

func (m *mockReportingRepo) AggregateCosts(ctx context.Context, from, to time.Time, window string) ([]domain.AggregatedCost, error) {
	return []domain.AggregatedCost{{Provider: domain.ProviderOCI, TotalCost: 100}}, nil
}
func (m *mockReportingRepo) CompareCostVariance(ctx context.Context, period string, currentFrom, currentTo, previousFrom, previousTo time.Time) ([]domain.CostVariance, error) {
	return []domain.CostVariance{{Period: period, Provider: domain.ProviderOCI, Service: "COMPUTE", CurrentCost: 100, PreviousCost: 80, Delta: 20, PercentChange: 25, Direction: "increase"}}, nil
}
func (m *mockReportingRepo) DetectAnomalies(ctx context.Context, from, to time.Time) ([]domain.Anomaly, error) {
	return []domain.Anomaly{{Provider: domain.ProviderOCI, Actual: 50}}, nil
}
func (m *mockReportingRepo) ForecastCosts(ctx context.Context, from, to time.Time, horizon int) ([]domain.ForecastPoint, error) {
	return []domain.ForecastPoint{{Provider: domain.ProviderOCI, ForecastCost: 150}}, nil
}
func (m *mockReportingRepo) StoreIngestedBatch(ctx context.Context, file domain.ProcessedReportFile, records []domain.CanonicalCostRecord) error {
	return nil
}
func (m *mockReportingRepo) StoreCostRecords(ctx context.Context, records []domain.CanonicalCostRecord) error {
	return nil
}
func (m *mockReportingRepo) DeleteCostRecordsForSource(ctx context.Context, provider domain.Provider, sourceObject string) error {
	return nil
}
func (m *mockReportingRepo) MarkReportProcessed(ctx context.Context, file domain.ProcessedReportFile) error {
	return nil
}
func (m *mockReportingRepo) LastIngestionCheckpoint(ctx context.Context, provider domain.Provider) (time.Time, error) {
	return time.Time{}, nil
}
func (m *mockReportingRepo) MarkIngestionCheckpoint(ctx context.Context, provider domain.Provider, checkpoint time.Time) error {
	return nil
}
func (m *mockReportingRepo) IsReportProcessed(ctx context.Context, provider domain.Provider, bucket, objectName, etag string) (bool, error) {
	return false, nil
}
func (m *mockReportingRepo) IsReportDelivered(ctx context.Context, key domain.ReportDeliveryKey) (bool, error) {
	return false, nil
}
func (m *mockReportingRepo) RecordReportDelivery(ctx context.Context, key domain.ReportDeliveryKey) error {
	return nil
}
func (m *mockReportingRepo) ApplyDataLifecyclePolicies(ctx context.Context, retentionDays, compressionAfterDays int) error {
	return nil
}
func (m *mockReportingRepo) RunDataLifecycleMaintenance(ctx context.Context, retentionDays, compressionAfterDays int) (domain.DataLifecycleMaintenance, error) {
	return domain.DataLifecycleMaintenance{}, nil
}
func (m *mockReportingRepo) RefreshAggregates(ctx context.Context, from, to time.Time) error {
	return nil
}
func (m *mockReportingRepo) RefreshDashboardAnalytics(ctx context.Context, from, to time.Time, retentionDays int) error {
	return nil
}
func (m *mockReportingRepo) DashboardOverview(ctx context.Context, currentFrom, currentTo, previousFrom, previousTo time.Time) (domain.DashboardOverview, error) {
	return domain.DashboardOverview{}, nil
}
func (m *mockReportingRepo) DashboardCostSummaries(ctx context.Context, window string, from, to time.Time) ([]domain.DashboardCostSummary, error) {
	return []domain.DashboardCostSummary{}, nil
}
func (m *mockReportingRepo) DashboardAnomalies(ctx context.Context, from, to time.Time, severity string) ([]domain.DashboardAnomaly, error) {
	return []domain.DashboardAnomaly{}, nil
}
func (m *mockReportingRepo) LatestIngestionStatus(ctx context.Context) (domain.IngestionStatusSummary, error) {
	return domain.IngestionStatusSummary{Providers: []domain.ProviderIngestionStatus{}}, nil
}

func TestBuildReport(t *testing.T) {
	repo := &mockReportingRepo{}
	analyticsSvc := analytics.New(repo)
	forecastSvc := forecasting.New(repo)
	reportingSvc := reporting.New(analyticsSvc, forecastSvc)

	report, err := reportingSvc.Build(context.Background(), "daily", time.Now(), time.Now())
	if err != nil {
		t.Fatalf("build report error: %v", err)
	}

	if report.Period != "daily" {
		t.Errorf("expected daily period, got %s", report.Period)
	}
	if len(report.Summary) != 1 {
		t.Errorf("expected 1 summary item, got %d", len(report.Summary))
	}
	if len(report.Anomalies) != 1 {
		t.Errorf("expected 1 anomaly item, got %d", len(report.Anomalies))
	}
	if len(report.Forecast) != 1 {
		t.Errorf("expected 1 forecast item, got %d", len(report.Forecast))
	}
}
