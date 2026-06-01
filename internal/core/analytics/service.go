package analytics

import (
	"context"
	"math"
	"time"

	"github.com/crypticani/torvix/internal/domain"
	"github.com/crypticani/torvix/internal/ports/storage"
)

type Service struct {
	repo storage.Repository
}

func New(repo storage.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Aggregate(ctx context.Context, from, to time.Time) ([]domain.AggregatedCost, error) {
	return s.repo.AggregateCosts(ctx, from, to, "daily")
}

func (s *Service) AggregateWindow(ctx context.Context, from, to time.Time, window string) ([]domain.AggregatedCost, error) {
	return s.repo.AggregateCosts(ctx, from, to, window)
}

func (s *Service) CompareVariance(ctx context.Context, period string, now time.Time) ([]domain.CostVariance, error) {
	currentFrom, currentTo, previousFrom, previousTo := comparisonWindows(period, now.UTC())
	return s.repo.CompareCostVariance(ctx, period, currentFrom, currentTo, previousFrom, previousTo)
}

func (s *Service) CompareVarianceWindows(ctx context.Context, period string, currentFrom, currentTo, previousFrom, previousTo time.Time) ([]domain.CostVariance, error) {
	return s.repo.CompareCostVariance(ctx, period, currentFrom.UTC(), currentTo.UTC(), previousFrom.UTC(), previousTo.UTC())
}

func (s *Service) IsReportDelivered(ctx context.Context, key domain.ReportDeliveryKey) (bool, error) {
	key.PeriodStart = key.PeriodStart.UTC()
	key.PeriodEnd = key.PeriodEnd.UTC()
	return s.repo.IsReportDelivered(ctx, key)
}

func (s *Service) RecordReportDelivery(ctx context.Context, key domain.ReportDeliveryKey) error {
	key.PeriodStart = key.PeriodStart.UTC()
	key.PeriodEnd = key.PeriodEnd.UTC()
	return s.repo.RecordReportDelivery(ctx, key)
}

func (s *Service) DetectAnomalies(ctx context.Context, from, to time.Time) ([]domain.Anomaly, error) {
	return s.repo.DetectAnomalies(ctx, from, to)
}

func (s *Service) DashboardOverview(ctx context.Context, now time.Time) (domain.DashboardOverview, error) {
	now = now.UTC()
	currentTo := now
	currentFrom := currentTo.AddDate(0, 0, -30)
	previousTo := currentFrom
	previousFrom := previousTo.AddDate(0, 0, -30)
	return s.repo.DashboardOverview(ctx, currentFrom, currentTo, previousFrom, previousTo)
}

func (s *Service) DashboardCostSummaries(ctx context.Context, window string, from, to time.Time) ([]domain.DashboardCostSummary, error) {
	return s.repo.DashboardCostSummaries(ctx, window, from, to)
}

func (s *Service) DashboardAnomalies(ctx context.Context, from, to time.Time, severity string) ([]domain.DashboardAnomaly, error) {
	return s.repo.DashboardAnomalies(ctx, from, to, severity)
}

func (s *Service) LatestIngestionStatus(ctx context.Context) (domain.IngestionStatusSummary, error) {
	return s.repo.LatestIngestionStatus(ctx)
}

func detectSeriesAnomalies(series []domain.AggregatedCost) []domain.Anomaly {
	if len(series) < 7 {
		return nil
	}

	var out []domain.Anomaly
	for i := 6; i < len(series); i++ {
		window := series[i-6 : i]
		mean, stddev := stats(window)
		actual := series[i].TotalCost
		z := 0.0
		if stddev > 0 {
			z = (actual - mean) / stddev
		}
		pct := 0.0
		if mean > 0 {
			pct = ((actual - mean) / mean) * 100
		}
		delta := actual - mean
		if math.Abs(z) >= 2 || math.Abs(pct) >= 30 || math.Abs(delta) >= mean*0.25 {
			severity := "medium"
			if math.Abs(z) >= 3 || math.Abs(pct) >= 50 {
				severity = "high"
			}
			out = append(out, domain.Anomaly{
				Date:               series[i].WindowStart,
				Provider:           series[i].Provider,
				AccountID:          series[i].AccountID,
				Service:            series[i].Service,
				Baseline:           mean,
				Actual:             actual,
				ZScore:             z,
				PercentDeviation:   pct,
				MovingAverageDelta: delta,
				Severity:           severity,
			})
		}
	}
	return out
}

func stats(items []domain.AggregatedCost) (mean, stddev float64) {
	if len(items) == 0 {
		return 0, 0
	}
	for _, v := range items {
		mean += v.TotalCost
	}
	mean /= float64(len(items))
	for _, v := range items {
		stddev += math.Pow(v.TotalCost-mean, 2)
	}
	stddev = math.Sqrt(stddev / float64(len(items)))
	return mean, stddev
}

func comparisonWindows(period string, now time.Time) (currentFrom, currentTo, previousFrom, previousTo time.Time) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	switch period {
	case "weekly":
		weekday := int(today.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		thisWeekStart := today.AddDate(0, 0, -(weekday - 1))
		currentTo = thisWeekStart
		currentFrom = currentTo.AddDate(0, 0, -7)
		previousTo = currentFrom
		previousFrom = previousTo.AddDate(0, 0, -7)
	case "monthly":
		thisMonthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
		currentTo = thisMonthStart
		currentFrom = currentTo.AddDate(0, -1, 0)
		previousTo = currentFrom
		previousFrom = previousTo.AddDate(0, -1, 0)
	default:
		currentTo = today
		currentFrom = currentTo.AddDate(0, 0, -1)
		previousTo = currentFrom
		previousFrom = previousTo.AddDate(0, 0, -1)
	}
	return currentFrom, currentTo, previousFrom, previousTo
}
