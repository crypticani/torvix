package app

import (
	"context"
	"io"
	"log/slog"
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

func TestReportCronDailyRunsOncePerDayAtConfiguredLocalTime(t *testing.T) {
	loc := mustLocation(t, "Asia/Kolkata")
	cron, err := parseReportCron("0 14 * * *", loc)
	if err != nil {
		t.Fatalf("parseReportCron() error = %v", err)
	}

	sameDay := cron.Next(time.Date(2026, 6, 1, 13, 59, 0, 0, loc))
	if !sameDay.Equal(time.Date(2026, 6, 1, 14, 0, 0, 0, loc)) {
		t.Fatalf("next daily before cutoff = %s, want 2026-06-01 14:00 IST", sameDay)
	}

	nextDay := cron.Next(time.Date(2026, 6, 1, 14, 1, 0, 0, loc))
	if !nextDay.Equal(time.Date(2026, 6, 2, 14, 0, 0, 0, loc)) {
		t.Fatalf("next daily after cutoff = %s, want 2026-06-02 14:00 IST", nextDay)
	}
}

func TestReportCronWeeklyRunsOnMonday(t *testing.T) {
	loc := mustLocation(t, "Asia/Kolkata")
	cron, err := parseReportCron("0 15 * * 1", loc)
	if err != nil {
		t.Fatalf("parseReportCron() error = %v", err)
	}

	next := cron.Next(time.Date(2026, 5, 31, 12, 0, 0, 0, loc))
	want := time.Date(2026, 6, 1, 15, 0, 0, 0, loc)
	if !next.Equal(want) {
		t.Fatalf("next weekly = %s, want %s", next, want)
	}
}

func TestDeliverScheduledReportSendsOnlyRequestedPeriod(t *testing.T) {
	loc := mustLocation(t, "Asia/Kolkata")
	targetStart := time.Date(2026, 5, 31, 0, 0, 0, 0, loc).UTC()
	repo := &scheduledReportRepo{aggregateByDay: map[time.Time][]domain.AggregatedCost{
		targetStart: {{WindowStart: targetStart, WindowEnd: targetStart.AddDate(0, 0, 1), Provider: domain.ProviderOCI, Service: "daily", TotalCost: 10}},
	}}
	reportingSvc := reporting.NewWithOptions(analytics.New(repo), forecasting.New(repo), reporting.Options{
		Location:                 loc,
		DailyReportTargetLagDays: 1,
		RequireCompleteIngestion: true,
	})
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

	a.deliverScheduledReport(context.Background(), "daily", time.Date(2026, 6, 1, 14, 0, 0, 0, loc))

	if posts.Load() != 1 {
		t.Fatalf("expected only daily report post, got %d", posts.Load())
	}
	if len(repo.reportWindows) == 0 {
		t.Fatalf("expected daily report aggregate calls, got none")
	}
	for _, window := range repo.reportWindows {
		if window != "daily" {
			t.Fatalf("expected only daily report aggregate calls, got %v", repo.reportWindows)
		}
	}
}

func TestScheduledIngestionSendsCompletionAlertAndCatchesUpDueDailyReport(t *testing.T) {
	loc := mustLocation(t, "Asia/Kolkata")
	targetStart := time.Date(2026, 6, 1, 0, 0, 0, 0, loc).UTC()
	repo := &scheduledReportRepo{aggregateByDay: map[time.Time][]domain.AggregatedCost{
		targetStart: {{WindowStart: targetStart, WindowEnd: targetStart.AddDate(0, 0, 1), Provider: domain.ProviderOCI, Service: "Object Storage", TotalCost: 10}},
	}}
	reportingSvc := reporting.NewWithOptions(analytics.New(repo), forecasting.New(repo), reporting.Options{
		Location:                 loc,
		DailyReportTargetLagDays: 1,
		RequireCompleteIngestion: true,
	})
	var posts atomic.Int32
	alertingSvc := alerting.New(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		posts.Add(1)
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Status:     "204 No Content",
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	})}, []config.Webhook{{Name: "discord-finops", Type: "discord", URL: "https://example.test/webhook", Enabled: true}})
	a := &App{
		collector: fakeIngestionRunner{results: []collect.ProviderResult{{
			Provider:              "oci",
			FilesProcessed:        1,
			RecordsParsed:         100,
			RecordsWithinLookback: 100,
			RecordsInserted:       100,
			Duration:              time.Second,
		}}},
		reporting:       reportingSvc,
		alerting:        alertingSvc,
		reportingCfg:    config.Reporting{Timezone: "Asia/Kolkata", DailyReportCron: "0 14 * * *", WeeklyReportCron: "0 15 * * 1"},
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		schedulerLogger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	now := time.Date(2026, 6, 2, 16, 0, 0, 0, loc)
	a.runScheduledIngestion(context.Background(), func() time.Time { return now })

	if posts.Load() != 2 {
		t.Fatalf("expected scheduled ingestion alert and due daily report delivery, got %d webhook posts", posts.Load())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type fakeIngestionRunner struct {
	results []collect.ProviderResult
	err     error
}

func (r fakeIngestionRunner) Run(context.Context, time.Time) ([]collect.ProviderResult, error) {
	return r.results, r.err
}

type scheduledReportRepo struct {
	aggregateByDay map[time.Time][]domain.AggregatedCost
	reportWindows  []string
	delivered      map[string]bool
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
	r.reportWindows = append(r.reportWindows, window)
	if window == "daily" && r.aggregateByDay != nil {
		if rows, ok := r.aggregateByDay[from.UTC()]; ok {
			return rows, nil
		}
		return []domain.AggregatedCost{}, nil
	}
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
func (r *scheduledReportRepo) IsReportDelivered(_ context.Context, key domain.ReportDeliveryKey) (bool, error) {
	if r.delivered == nil {
		return false, nil
	}
	return r.delivered[reportDeliveryKey(key)], nil
}
func (r *scheduledReportRepo) RecordReportDelivery(_ context.Context, key domain.ReportDeliveryKey) error {
	if r.delivered == nil {
		r.delivered = make(map[string]bool)
	}
	r.delivered[reportDeliveryKey(key)] = true
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

func reportDeliveryKey(key domain.ReportDeliveryKey) string {
	return key.Provider + "|" + key.ReportType + "|" + key.PeriodStart.UTC().Format(time.RFC3339) + "|" + key.PeriodEnd.UTC().Format(time.RFC3339) + "|" + key.Destination
}

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load location %s: %v", name, err)
	}
	return loc
}
