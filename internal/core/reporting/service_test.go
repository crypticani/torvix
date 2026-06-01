package reporting

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/crypticani/torvix/internal/core/analytics"
	"github.com/crypticani/torvix/internal/core/forecasting"
	"github.com/crypticani/torvix/internal/domain"
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

func TestDefaultRangeWithReportTimezoneAndLag(t *testing.T) {
	loc := mustLocation(t, "Asia/Kolkata")
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, loc)

	dailyFrom, dailyTo := DefaultRangeWithOptions("daily", now, Options{Location: loc, DailyReportTargetLagDays: 1})
	wantDailyFrom := time.Date(2026, 5, 31, 0, 0, 0, 0, loc).UTC()
	wantDailyTo := time.Date(2026, 6, 1, 0, 0, 0, 0, loc).UTC()
	if !dailyFrom.Equal(wantDailyFrom) || !dailyTo.Equal(wantDailyTo) {
		t.Fatalf("daily range = %s - %s, want %s - %s", dailyFrom, dailyTo, wantDailyFrom, wantDailyTo)
	}

	weeklyFrom, weeklyTo := DefaultRangeWithOptions("weekly", now, Options{Location: loc, DailyReportTargetLagDays: 1})
	wantWeeklyFrom := time.Date(2026, 5, 25, 0, 0, 0, 0, loc).UTC()
	wantWeeklyTo := time.Date(2026, 6, 1, 0, 0, 0, 0, loc).UTC()
	if !weeklyFrom.Equal(wantWeeklyFrom) || !weeklyTo.Equal(wantWeeklyTo) {
		t.Fatalf("weekly range = %s - %s, want %s - %s", weeklyFrom, weeklyTo, wantWeeklyFrom, wantWeeklyTo)
	}
}

func TestDeliverScheduledDailySkipsIncompleteTargetDate(t *testing.T) {
	loc := mustLocation(t, "Asia/Kolkata")
	repo := &reportingRepo{aggregateByDay: map[time.Time][]domain.AggregatedCost{}}
	svc := NewWithOptions(analytics.New(repo), forecasting.New(repo), Options{
		Location:                 loc,
		DailyReportTargetLagDays: 1,
		RequireCompleteIngestion: true,
	})
	sender := &recordingReportSender{destinations: []string{"slack"}}

	results := svc.DeliverScheduledReport(context.Background(), sender, "daily", time.Date(2026, 6, 1, 12, 0, 0, 0, loc), DeliverOptions{})

	if len(results) != 1 || !results[0].Skipped || results[0].SkipReason != SkipDailyIncomplete {
		t.Fatalf("expected incomplete daily skip, got %+v", results)
	}
	if len(sender.reports) != 0 {
		t.Fatalf("expected no reports sent, got %d", len(sender.reports))
	}
}

func TestDeliverScheduledWeeklySkipsIncompleteRange(t *testing.T) {
	loc := mustLocation(t, "Asia/Kolkata")
	repo := &reportingRepo{aggregateByDay: map[time.Time][]domain.AggregatedCost{}}
	for day := 25; day <= 30; day++ {
		start := time.Date(2026, 5, day, 0, 0, 0, 0, loc).UTC()
		repo.aggregateByDay[start] = []domain.AggregatedCost{{WindowStart: start, WindowEnd: start.AddDate(0, 0, 1), Provider: domain.ProviderOCI, Service: "Compute", TotalCost: 10}}
	}
	svc := NewWithOptions(analytics.New(repo), forecasting.New(repo), Options{
		Location:                 loc,
		DailyReportTargetLagDays: 1,
		RequireCompleteIngestion: true,
	})
	sender := &recordingReportSender{destinations: []string{"slack"}}

	results := svc.DeliverScheduledReport(context.Background(), sender, "weekly", time.Date(2026, 6, 1, 15, 0, 0, 0, loc), DeliverOptions{})

	if len(results) != 1 || !results[0].Skipped || results[0].SkipReason != SkipWeeklyIncomplete {
		t.Fatalf("expected incomplete weekly skip, got %+v", results)
	}
	if len(sender.reports) != 0 {
		t.Fatalf("expected no reports sent, got %d", len(sender.reports))
	}
}

func TestDeliverScheduledReportRecordsAndSkipsDuplicatePerDestination(t *testing.T) {
	loc := mustLocation(t, "Asia/Kolkata")
	targetStart := time.Date(2026, 5, 31, 0, 0, 0, 0, loc).UTC()
	repo := &reportingRepo{
		aggregateByDay: map[time.Time][]domain.AggregatedCost{
			targetStart: {{WindowStart: targetStart, WindowEnd: targetStart.AddDate(0, 0, 1), Provider: domain.ProviderOCI, Service: "Compute", TotalCost: 10}},
		},
		delivered: make(map[string]bool),
	}
	svc := NewWithOptions(analytics.New(repo), forecasting.New(repo), Options{
		Location:                 loc,
		DailyReportTargetLagDays: 1,
		RequireCompleteIngestion: true,
	})
	sender := &recordingReportSender{destinations: []string{"slack"}}
	now := time.Date(2026, 6, 1, 14, 0, 0, 0, loc)

	first := svc.DeliverScheduledReport(context.Background(), sender, "daily", now, DeliverOptions{})
	second := svc.DeliverScheduledReport(context.Background(), sender, "daily", now, DeliverOptions{})

	if len(first) != 1 || first[0].Error != nil || first[0].Skipped {
		t.Fatalf("expected first delivery success, got %+v", first)
	}
	if len(second) != 1 || !second[0].Skipped || second[0].SkipReason != "report already delivered" {
		t.Fatalf("expected duplicate skip, got %+v", second)
	}
	if len(sender.reports) != 1 {
		t.Fatalf("expected one sent report, got %d", len(sender.reports))
	}
	key := deliveryKey("all", "daily", targetStart, targetStart.AddDate(0, 0, 1), "slack")
	if !repo.recorded[key] {
		t.Fatalf("expected delivery key %s to be recorded, got %#v", key, repo.recorded)
	}
}

func TestDeliverDefaultReportsContinuesAfterOneReportFails(t *testing.T) {
	repo := &reportingRepo{}
	svc := NewWithOptions(analytics.New(repo), forecasting.New(repo), Options{RequireCompleteIngestion: false})
	sender := &recordingReportSender{destinations: []string{"slack"}, failPeriod: "weekly"}

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

func TestBuildDefaultIncludesTopCostIncreasesAndDecreases(t *testing.T) {
	repo := &reportingRepo{
		variances: []domain.CostVariance{
			{Provider: domain.ProviderOCI, Service: "COMPUTE", CompartmentName: "app", CurrentCost: 300, PreviousCost: 100, Delta: 200, PercentChange: 200, Direction: "increase"},
			{Provider: domain.ProviderOCI, Service: "OBJECTSTORE", CompartmentName: "data", CurrentCost: 40, PreviousCost: 140, Delta: -100, PercentChange: -71.4, Direction: "decrease"},
			{Provider: domain.ProviderOCI, Service: "NETWORK", CompartmentName: "shared", CurrentCost: 50, PreviousCost: 50, Delta: 0, Direction: "flat"},
		},
	}
	svc := NewWithOptions(analytics.New(repo), forecasting.New(repo), Options{RequireCompleteIngestion: false})

	report, err := svc.BuildDefault(context.Background(), "weekly", time.Date(2026, 5, 17, 15, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("BuildDefault() error = %v", err)
	}

	if len(report.CostIncreases) != 1 || report.CostIncreases[0].Service != "COMPUTE" {
		t.Fatalf("expected COMPUTE top increase, got %+v", report.CostIncreases)
	}
	if len(report.CostDecreases) != 1 || report.CostDecreases[0].Service != "OBJECTSTORE" {
		t.Fatalf("expected OBJECTSTORE top decrease, got %+v", report.CostDecreases)
	}
}

type recordingReportSender struct {
	destinations []string
	reports      []domain.Report
	failPeriod   string
}

func (s *recordingReportSender) ReportDestinations() []string {
	return s.destinations
}

func (s *recordingReportSender) SendReport(_ context.Context, report domain.Report) error {
	s.reports = append(s.reports, report)
	if report.Period == s.failPeriod {
		return errors.New("delivery failed")
	}
	return nil
}

func (s *recordingReportSender) SendReportToDestination(ctx context.Context, _ string, report domain.Report) error {
	return s.SendReport(ctx, report)
}

type aggregateCall struct {
	from   time.Time
	to     time.Time
	window string
}

type reportingRepo struct {
	aggregateCalls []aggregateCall
	aggregateByDay map[time.Time][]domain.AggregatedCost
	delivered      map[string]bool
	recorded       map[string]bool
	variances      []domain.CostVariance
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
	return r.variances, nil
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

func (r *reportingRepo) IsReportDelivered(_ context.Context, key domain.ReportDeliveryKey) (bool, error) {
	if r.delivered == nil {
		return false, nil
	}
	return r.delivered[deliveryKey(key.Provider, key.ReportType, key.PeriodStart, key.PeriodEnd, key.Destination)], nil
}

func (r *reportingRepo) RecordReportDelivery(_ context.Context, key domain.ReportDeliveryKey) error {
	if r.recorded == nil {
		r.recorded = make(map[string]bool)
	}
	if r.delivered == nil {
		r.delivered = make(map[string]bool)
	}
	keyString := deliveryKey(key.Provider, key.ReportType, key.PeriodStart, key.PeriodEnd, key.Destination)
	r.recorded[keyString] = true
	r.delivered[keyString] = true
	return nil
}

func deliveryKey(provider, period string, from, to time.Time, destination string) string {
	return fmt.Sprintf("%s|%s|%s|%s|%s", provider, period, from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339), destination)
}

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load location %s: %v", name, err)
	}
	return loc
}
