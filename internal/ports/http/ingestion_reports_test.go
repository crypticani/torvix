package httpapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/crypticani/cloudpulse/internal/config"
	"github.com/crypticani/cloudpulse/internal/core/alerting"
	"github.com/crypticani/cloudpulse/internal/core/analytics"
	"github.com/crypticani/cloudpulse/internal/core/forecasting"
	"github.com/crypticani/cloudpulse/internal/core/reporting"
	"github.com/crypticani/cloudpulse/internal/domain"
)

func TestDeliverIngestionReportsSendsAllDefaultReportsForSuccessfulJob(t *testing.T) {
	repo := &ingestionReportRepo{}
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
	h := NewWithOptions(nil, analytics.New(repo), forecasting.New(repo), reportingSvc, alertingSvc, prometheus.NewRegistry(), HandlerOptions{})

	h.deliverIngestionReports(IngestionJobResponse{Status: ingestionStatusSuccess})

	if posts.Load() != 3 {
		t.Fatalf("expected daily, weekly, and monthly report posts, got %d", posts.Load())
	}
	if len(repo.aggregateWindows) != 3 {
		t.Fatalf("expected three report builds, got %d", len(repo.aggregateWindows))
	}
}

func TestDeliverIngestionReportsSkipsFailedJobs(t *testing.T) {
	repo := &ingestionReportRepo{}
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
	h := NewWithOptions(nil, analytics.New(repo), forecasting.New(repo), reportingSvc, alertingSvc, prometheus.NewRegistry(), HandlerOptions{})

	h.deliverIngestionReports(IngestionJobResponse{Status: ingestionStatusFailed})

	if posts.Load() != 0 {
		t.Fatalf("expected no report posts for failed ingestion, got %d", posts.Load())
	}
}

func TestDeliverIngestionReportsSkipsPartialJobs(t *testing.T) {
	repo := &ingestionReportRepo{}
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
	h := NewWithOptions(nil, analytics.New(repo), forecasting.New(repo), reportingSvc, alertingSvc, prometheus.NewRegistry(), HandlerOptions{})

	h.deliverIngestionReports(IngestionJobResponse{Status: ingestionStatusPartial})

	if posts.Load() != 0 {
		t.Fatalf("expected no report posts for partial ingestion, got %d", posts.Load())
	}
}

type ingestionReportRepo struct {
	aggregateWindows []string
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func (r *ingestionReportRepo) StoreIngestedBatch(context.Context, domain.ProcessedReportFile, []domain.CanonicalCostRecord) error {
	return nil
}
func (r *ingestionReportRepo) StoreCostRecords(context.Context, []domain.CanonicalCostRecord) error {
	return nil
}
func (r *ingestionReportRepo) DeleteCostRecordsForSource(context.Context, domain.Provider, string) error {
	return nil
}
func (r *ingestionReportRepo) MarkReportProcessed(context.Context, domain.ProcessedReportFile) error {
	return nil
}
func (r *ingestionReportRepo) LastIngestionCheckpoint(context.Context, domain.Provider) (time.Time, error) {
	return time.Time{}, nil
}
func (r *ingestionReportRepo) MarkIngestionCheckpoint(context.Context, domain.Provider, time.Time) error {
	return nil
}
func (r *ingestionReportRepo) AggregateCosts(_ context.Context, from, to time.Time, window string) ([]domain.AggregatedCost, error) {
	r.aggregateWindows = append(r.aggregateWindows, window)
	return []domain.AggregatedCost{{WindowStart: from, WindowEnd: to, Provider: domain.ProviderOCI, Service: window, TotalCost: 10}}, nil
}
func (r *ingestionReportRepo) CompareCostVariance(context.Context, string, time.Time, time.Time, time.Time, time.Time) ([]domain.CostVariance, error) {
	return nil, nil
}
func (r *ingestionReportRepo) DetectAnomalies(context.Context, time.Time, time.Time) ([]domain.Anomaly, error) {
	return nil, nil
}
func (r *ingestionReportRepo) ForecastCosts(_ context.Context, from, _ time.Time, _ int) ([]domain.ForecastPoint, error) {
	return []domain.ForecastPoint{{Date: from, Provider: domain.ProviderOCI, Service: "forecast", ForecastCost: 10}}, nil
}
func (r *ingestionReportRepo) IsReportProcessed(context.Context, domain.Provider, string, string, string) (bool, error) {
	return false, nil
}
func (r *ingestionReportRepo) IsReportDelivered(context.Context, string, time.Time, time.Time) (bool, error) {
	return false, nil
}
func (r *ingestionReportRepo) RecordReportDelivery(context.Context, string, time.Time, time.Time) error {
	return nil
}
func (r *ingestionReportRepo) ApplyDataLifecyclePolicies(context.Context, int, int) error {
	return nil
}
func (r *ingestionReportRepo) RunDataLifecycleMaintenance(context.Context, int, int) (domain.DataLifecycleMaintenance, error) {
	return domain.DataLifecycleMaintenance{}, nil
}
func (r *ingestionReportRepo) RefreshAggregates(context.Context, time.Time, time.Time) error {
	return nil
}
func (r *ingestionReportRepo) RefreshDashboardAnalytics(context.Context, time.Time, time.Time, int) error {
	return nil
}
func (r *ingestionReportRepo) DashboardOverview(context.Context, time.Time, time.Time, time.Time, time.Time) (domain.DashboardOverview, error) {
	return domain.DashboardOverview{}, nil
}
func (r *ingestionReportRepo) DashboardCostSummaries(context.Context, string, time.Time, time.Time) ([]domain.DashboardCostSummary, error) {
	return nil, nil
}
func (r *ingestionReportRepo) DashboardAnomalies(context.Context, time.Time, time.Time, string) ([]domain.DashboardAnomaly, error) {
	return nil, nil
}
func (r *ingestionReportRepo) LatestIngestionStatus(context.Context) (domain.IngestionStatusSummary, error) {
	return domain.IngestionStatusSummary{}, nil
}
