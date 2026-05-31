package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crypticani/torvix/internal/config"
	"github.com/crypticani/torvix/internal/core/alerting"
	"github.com/crypticani/torvix/internal/core/analytics"
	"github.com/crypticani/torvix/internal/core/collect"
	"github.com/crypticani/torvix/internal/core/forecasting"
	"github.com/crypticani/torvix/internal/core/reporting"
	"github.com/crypticani/torvix/internal/domain"
)

func TestDeliverScheduledReportsSendsDailyWeeklyAndMonthly(t *testing.T) {
	repo := &scheduledReportRepo{}
	reportingSvc := reporting.New(analytics.New(repo), forecasting.New(repo))
	var posts atomic.Int32
	alertingSvc := alerting.New(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		posts.Add(1)
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Status:     "204 No Content",
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	})}, []config.Webhook{{Name: "slack", Type: "slack", URL: "https://example.test/webhook", Enabled: true}})
	a := &App{reporting: reportingSvc, alerting: alertingSvc}

	a.deliverScheduledReports(context.Background())

	if posts.Load() != 3 {
		t.Fatalf("expected daily, weekly, and monthly report posts, got %d", posts.Load())
	}
	if len(repo.aggregateWindows) != 3 {
		t.Fatalf("expected three report builds, got %d", len(repo.aggregateWindows))
	}
}

func TestShouldDeliverScheduledReportsOnlyAfterSuccessfulIngestion(t *testing.T) {
	if !shouldDeliverScheduledReports([]collect.ProviderResult{{Provider: "oci"}}, nil) {
		t.Fatal("expected reports after successful ingestion")
	}
	if shouldDeliverScheduledReports([]collect.ProviderResult{{Provider: "oci", Error: "parse failed"}}, errors.New("parse failed")) {
		t.Fatal("expected no reports after partial ingestion")
	}
	if shouldDeliverScheduledReports(nil, errors.New("database unavailable")) {
		t.Fatal("expected no reports after failed ingestion")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type scheduledReportRepo struct {
	aggregateWindows []string
}

func (r *scheduledReportRepo) StoreIngestedBatch(context.Context, domain.ProcessedReportFile, []domain.CanonicalCostRecord) error {
	return nil
}
func (r *scheduledReportRepo) StoreCostRecords(context.Context, []domain.CanonicalCostRecord) error {
	return nil
}
func (r *scheduledReportRepo) DeleteCostRecordsForSource(context.Context, domain.Provider, string) error {
	return nil
}
func (r *scheduledReportRepo) MarkReportProcessed(context.Context, domain.ProcessedReportFile) error {
	return nil
}
func (r *scheduledReportRepo) LastIngestionCheckpoint(context.Context, domain.Provider) (time.Time, error) {
	return time.Time{}, nil
}
func (r *scheduledReportRepo) MarkIngestionCheckpoint(context.Context, domain.Provider, time.Time) error {
	return nil
}
func (r *scheduledReportRepo) AggregateCosts(_ context.Context, from, to time.Time, window string) ([]domain.AggregatedCost, error) {
	r.aggregateWindows = append(r.aggregateWindows, window)
	return []domain.AggregatedCost{{WindowStart: from, WindowEnd: to, Provider: domain.ProviderOCI, Service: window, TotalCost: 10}}, nil
}
func (r *scheduledReportRepo) CompareCostVariance(context.Context, string, time.Time, time.Time, time.Time, time.Time) ([]domain.CostVariance, error) {
	return nil, nil
}
func (r *scheduledReportRepo) DetectAnomalies(context.Context, time.Time, time.Time) ([]domain.Anomaly, error) {
	return nil, nil
}
func (r *scheduledReportRepo) ForecastCosts(_ context.Context, from, _ time.Time, _ int) ([]domain.ForecastPoint, error) {
	return []domain.ForecastPoint{{Date: from, Provider: domain.ProviderOCI, Service: "forecast", ForecastCost: 10}}, nil
}
func (r *scheduledReportRepo) IsReportProcessed(context.Context, domain.Provider, string, string, string) (bool, error) {
	return false, nil
}
func (r *scheduledReportRepo) IsReportDelivered(context.Context, string, time.Time, time.Time) (bool, error) {
	return false, nil
}
func (r *scheduledReportRepo) RecordReportDelivery(context.Context, string, time.Time, time.Time) error {
	return nil
}
func (r *scheduledReportRepo) ApplyDataLifecyclePolicies(context.Context, int, int) error {
	return nil
}
func (r *scheduledReportRepo) RunDataLifecycleMaintenance(context.Context, int, int) (domain.DataLifecycleMaintenance, error) {
	return domain.DataLifecycleMaintenance{}, nil
}
func (r *scheduledReportRepo) RefreshAggregates(context.Context, time.Time, time.Time) error {
	return nil
}
func (r *scheduledReportRepo) RefreshDashboardAnalytics(context.Context, time.Time, time.Time, int) error {
	return nil
}
func (r *scheduledReportRepo) DashboardOverview(context.Context, time.Time, time.Time, time.Time, time.Time) (domain.DashboardOverview, error) {
	return domain.DashboardOverview{}, nil
}
func (r *scheduledReportRepo) DashboardCostSummaries(context.Context, string, time.Time, time.Time) ([]domain.DashboardCostSummary, error) {
	return nil, nil
}
func (r *scheduledReportRepo) DashboardAnomalies(context.Context, time.Time, time.Time, string) ([]domain.DashboardAnomaly, error) {
	return nil, nil
}
func (r *scheduledReportRepo) LatestIngestionStatus(context.Context) (domain.IngestionStatusSummary, error) {
	return domain.IngestionStatusSummary{}, nil
}
