package reporting

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/crypticani/cloudpulse/internal/core/analytics"
	"github.com/crypticani/cloudpulse/internal/core/forecasting"
	"github.com/crypticani/cloudpulse/internal/domain"
)

func TestDefaultRange(t *testing.T) {
	now := time.Date(2026, 5, 17, 15, 30, 0, 0, time.UTC)
	tests := []struct {
		period   string
		wantFrom time.Time
		wantTo   time.Time
	}{
		{
			period:   "daily",
			wantFrom: time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC),
			wantTo:   time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
		},
		{
			period:   "weekly",
			wantFrom: time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC),
			wantTo:   time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
		},
		{
			period:   "monthly",
			wantFrom: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			wantTo:   time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.period, func(t *testing.T) {
			gotFrom, gotTo := DefaultRange(tt.period, now)
			if !gotFrom.Equal(tt.wantFrom) || !gotTo.Equal(tt.wantTo) {
				t.Fatalf("DefaultRange() = %s - %s, want %s - %s", gotFrom, gotTo, tt.wantFrom, tt.wantTo)
			}
		})
	}
}

func TestDeliverDefaultReportsSendsDailyWeeklyAndMonthly(t *testing.T) {
	repo := &reportingRepo{}
	svc := New(analytics.New(repo), forecasting.New(repo))
	sender := &recordingReportSender{}
	now := time.Date(2026, 5, 17, 15, 30, 0, 0, time.UTC)

	results := svc.DeliverDefaultReports(context.Background(), sender, now)

	if len(results) != 3 {
		t.Fatalf("expected three delivery results, got %d", len(results))
	}
	if len(sender.reports) != 3 {
		t.Fatalf("expected three delivered reports, got %d", len(sender.reports))
	}
	gotPeriods := []string{sender.reports[0].Period, sender.reports[1].Period, sender.reports[2].Period}
	wantPeriods := []string{"daily", "weekly", "monthly"}
	if !reflect.DeepEqual(gotPeriods, wantPeriods) {
		t.Fatalf("delivered periods = %v, want %v", gotPeriods, wantPeriods)
	}
	wantRanges := map[string][2]time.Time{
		"daily": {
			time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
		},
		"weekly": {
			time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
		},
		"monthly": {
			time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, call := range repo.aggregateCalls {
		want, ok := wantRanges[call.window]
		if !ok {
			t.Fatalf("unexpected aggregate window %q", call.window)
		}
		if !call.from.Equal(want[0]) || !call.to.Equal(want[1]) {
			t.Fatalf("%s aggregate range = %s - %s, want %s - %s", call.window, call.from, call.to, want[0], want[1])
		}
	}
	for _, result := range results {
		if result.Error != nil {
			t.Fatalf("expected successful delivery result for %s, got %v", result.Period, result.Error)
		}
	}
}

func TestDeliverDefaultReportsContinuesAfterOneReportFails(t *testing.T) {
	repo := &reportingRepo{}
	svc := New(analytics.New(repo), forecasting.New(repo))
	sender := &recordingReportSender{failPeriod: "weekly"}

	results := svc.DeliverDefaultReports(context.Background(), sender, time.Date(2026, 5, 17, 15, 30, 0, 0, time.UTC))

	if len(results) != 3 {
		t.Fatalf("expected three delivery results, got %d", len(results))
	}
	if len(sender.reports) != 3 {
		t.Fatalf("expected delivery to continue after failure, got %d reports", len(sender.reports))
	}
	var failed []string
	for _, result := range results {
		if result.Error != nil {
			failed = append(failed, result.Period)
		}
	}
	if !reflect.DeepEqual(failed, []string{"weekly"}) {
		t.Fatalf("failed periods = %v, want [weekly]", failed)
	}
}

func TestBuildDefaultDailyFallsBackToLatestPriorDayWithData(t *testing.T) {
	repo := &reportingRepo{
		aggregateByDay: map[time.Time][]domain.AggregatedCost{
			time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC): {
				{
					WindowStart: time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC),
					WindowEnd:   time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC),
					Provider:    domain.ProviderOCI,
					Service:     "Compute",
					TotalCost:   42,
				},
			},
		},
	}
	svc := New(analytics.New(repo), forecasting.New(repo))

	report, err := svc.BuildDefault(context.Background(), "daily", time.Date(2026, 5, 22, 4, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("BuildDefault() error = %v", err)
	}

	if len(report.Summary) != 1 {
		t.Fatalf("expected fallback daily summary, got %d rows", len(report.Summary))
	}
	if !report.Summary[0].WindowStart.Equal(time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("expected latest prior day with data, got %s", report.Summary[0].WindowStart)
	}
}

func TestBuildDefaultDailySkipsExpensiveAnalyticsForEmptyFallbackDays(t *testing.T) {
	repo := &reportingRepo{
		aggregateByDay: map[time.Time][]domain.AggregatedCost{
			time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC): {
				{
					WindowStart: time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC),
					WindowEnd:   time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC),
					Provider:    domain.ProviderOCI,
					Service:     "Compute",
					TotalCost:   42,
				},
			},
		},
	}
	svc := New(analytics.New(repo), forecasting.New(repo))

	report, err := svc.BuildDefault(context.Background(), "daily", time.Date(2026, 5, 22, 4, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("BuildDefault() error = %v", err)
	}

	if len(report.Summary) != 1 {
		t.Fatalf("expected fallback daily summary, got %d rows", len(report.Summary))
	}
	if repo.detectCalls != 1 {
		t.Fatalf("DetectAnomalies calls = %d, want 1 for selected report day only", repo.detectCalls)
	}
	if repo.forecastCalls != 1 {
		t.Fatalf("ForecastCosts calls = %d, want 1 for selected report day only", repo.forecastCalls)
	}
}

type recordingReportSender struct {
	reports    []domain.Report
	failPeriod string
}

func (s *recordingReportSender) SendReport(_ context.Context, report domain.Report) error {
	s.reports = append(s.reports, report)
	if report.Period == s.failPeriod {
		return errors.New("delivery failed")
	}
	return nil
}

type aggregateCall struct {
	from   time.Time
	to     time.Time
	window string
}

type reportingRepo struct {
	aggregateCalls []aggregateCall
	aggregateByDay map[time.Time][]domain.AggregatedCost
	detectCalls    int
	forecastCalls  int
}

func (r *reportingRepo) StoreIngestedBatch(context.Context, domain.ProcessedReportFile, []domain.CanonicalCostRecord) error {
	return nil
}
func (r *reportingRepo) StoreCostRecords(context.Context, []domain.CanonicalCostRecord) error {
	return nil
}
func (r *reportingRepo) DeleteCostRecordsForSource(context.Context, domain.Provider, string) error {
	return nil
}
func (r *reportingRepo) MarkReportProcessed(context.Context, domain.ProcessedReportFile) error {
	return nil
}
func (r *reportingRepo) LastIngestionCheckpoint(context.Context, domain.Provider) (time.Time, error) {
	return time.Time{}, nil
}
func (r *reportingRepo) MarkIngestionCheckpoint(context.Context, domain.Provider, time.Time) error {
	return nil
}
func (r *reportingRepo) AggregateCosts(_ context.Context, from, to time.Time, window string) ([]domain.AggregatedCost, error) {
	r.aggregateCalls = append(r.aggregateCalls, aggregateCall{from: from, to: to, window: window})
	if window == "daily" && r.aggregateByDay != nil {
		if rows, ok := r.aggregateByDay[from.UTC()]; ok {
			return rows, nil
		}
		return []domain.AggregatedCost{}, nil
	}
	return []domain.AggregatedCost{{WindowStart: from, WindowEnd: to, Provider: domain.ProviderOCI, Service: window, TotalCost: 10}}, nil
}
func (r *reportingRepo) CompareCostVariance(context.Context, string, time.Time, time.Time, time.Time, time.Time) ([]domain.CostVariance, error) {
	return nil, nil
}
func (r *reportingRepo) DetectAnomalies(context.Context, time.Time, time.Time) ([]domain.Anomaly, error) {
	r.detectCalls++
	return nil, nil
}
func (r *reportingRepo) ForecastCosts(_ context.Context, from, _ time.Time, _ int) ([]domain.ForecastPoint, error) {
	r.forecastCalls++
	return []domain.ForecastPoint{{Date: from, Provider: domain.ProviderOCI, Service: "forecast", ForecastCost: 10}}, nil
}
func (r *reportingRepo) IsReportProcessed(context.Context, domain.Provider, string, string, string) (bool, error) {
	return false, nil
}
func (r *reportingRepo) ApplyDataLifecyclePolicies(context.Context, int, int) error {
	return nil
}
func (r *reportingRepo) RunDataLifecycleMaintenance(context.Context, int, int) (domain.DataLifecycleMaintenance, error) {
	return domain.DataLifecycleMaintenance{}, nil
}
func (r *reportingRepo) RefreshAggregates(context.Context, time.Time, time.Time) error {
	return nil
}
func (r *reportingRepo) RefreshDashboardAnalytics(context.Context, time.Time, time.Time, int) error {
	return nil
}
func (r *reportingRepo) DashboardOverview(context.Context, time.Time, time.Time, time.Time, time.Time) (domain.DashboardOverview, error) {
	return domain.DashboardOverview{}, nil
}
func (r *reportingRepo) DashboardCostSummaries(context.Context, string, time.Time, time.Time) ([]domain.DashboardCostSummary, error) {
	return nil, nil
}
func (r *reportingRepo) DashboardAnomalies(context.Context, time.Time, time.Time, string) ([]domain.DashboardAnomaly, error) {
	return nil, nil
}
func (r *reportingRepo) LatestIngestionStatus(context.Context) (domain.IngestionStatusSummary, error) {
	return domain.IngestionStatusSummary{}, nil
}
