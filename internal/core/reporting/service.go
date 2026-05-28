package reporting

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/crypticani/cloudpulse/internal/core/analytics"
	"github.com/crypticani/cloudpulse/internal/core/forecasting"
	"github.com/crypticani/cloudpulse/internal/domain"
)

var DefaultReportPeriods = []string{"daily", "weekly", "monthly"}

const maxCostMovements = 5

type ReportSender interface {
	SendReport(ctx context.Context, report domain.Report) error
}

type DeliveryResult struct {
	Period string
	From   time.Time
	To     time.Time
	Error  error
}

type Service struct {
	analytics   *analytics.Service
	forecasting *forecasting.Service
}

func New(a *analytics.Service, f *forecasting.Service) *Service {
	return &Service{analytics: a, forecasting: f}
}

func (s *Service) Build(ctx context.Context, period string, from, to time.Time) (domain.Report, error) {
	summary, err := s.analytics.AggregateWindow(ctx, from, to, period)
	if err != nil {
		return domain.Report{}, err
	}
	return s.buildWithSummary(ctx, period, from, to, summary)
}

func (s *Service) buildWithSummary(ctx context.Context, period string, from, to time.Time, summary []domain.AggregatedCost) (domain.Report, error) {
	anomalies, err := s.analytics.DetectAnomalies(ctx, from, to)
	if err != nil {
		return domain.Report{}, err
	}
	forecast, err := s.forecasting.Forecast(ctx, from, to, 7)
	if err != nil {
		return domain.Report{}, err
	}
	previousFrom, previousTo := previousReportRange(period, from, to)
	variances, err := s.analytics.CompareVarianceWindows(ctx, period, from, to, previousFrom, previousTo)
	if err != nil {
		return domain.Report{}, err
	}
	increases, decreases := topCostMovements(variances, maxCostMovements)
	return domain.Report{
		Period:        period,
		From:          from,
		To:            to,
		Generated:     time.Now().UTC(),
		Summary:       summary,
		Anomalies:     anomalies,
		Forecast:      forecast,
		CostIncreases: increases,
		CostDecreases: decreases,
	}, nil
}

func (s *Service) BuildDefault(ctx context.Context, period string, now time.Time) (domain.Report, error) {
	from, to := DefaultRange(period, now)
	summary, err := s.analytics.AggregateWindow(ctx, from, to, period)
	if err != nil {
		return domain.Report{}, err
	}
	if period != "daily" || len(summary) > 0 {
		return s.buildWithSummary(ctx, period, from, to, summary)
	}
	emptyReport := domain.Report{
		Period:    period,
		From:      from,
		To:        to,
		Generated: time.Now().UTC(),
		Summary:   summary,
	}
	for i := 0; i < 7; i++ {
		to = from
		from = from.AddDate(0, 0, -1)
		summary, err = s.analytics.AggregateWindow(ctx, from, to, period)
		if err != nil {
			return domain.Report{}, err
		}
		if len(summary) > 0 {
			return s.buildWithSummary(ctx, period, from, to, summary)
		}
	}
	return emptyReport, nil
}

func (s *Service) DeliverDefaultReports(ctx context.Context, sender ReportSender, now time.Time) []DeliveryResult {
	results := make([]DeliveryResult, 0, len(DefaultReportPeriods))
	if sender == nil {
		for _, period := range DefaultReportPeriods {
			from, to := DefaultRange(period, now)
			results = append(results, DeliveryResult{Period: period, From: from, To: to, Error: fmt.Errorf("report sender is not configured")})
		}
		return results
	}
	for _, period := range DefaultReportPeriods {
		from, to := DefaultRange(period, now)
		result := DeliveryResult{Period: period, From: from, To: to}
		deliver, err := s.shouldDeliverDefaultReport(ctx, period, from, to)
		if err != nil {
			result.Error = err
			results = append(results, result)
			continue
		}
		if !deliver {
			results = append(results, result)
			continue
		}
		report, err := s.BuildDefault(ctx, period, now)
		if err != nil {
			result.Error = err
			results = append(results, result)
			continue
		}
		result.From = report.From
		result.To = report.To
		if err := sender.SendReport(ctx, report); err != nil {
			result.Error = err
			results = append(results, result)
			continue
		}
		if err := s.recordDefaultReportDelivery(ctx, period, report.From, report.To); err != nil {
			result.Error = err
		}
		results = append(results, result)
	}
	return results
}

func (s *Service) shouldDeliverDefaultReport(ctx context.Context, period string, from, to time.Time) (bool, error) {
	if period == "daily" {
		return true, nil
	}
	delivered, err := s.analytics.IsReportDelivered(ctx, period, from, to)
	if err != nil {
		return false, err
	}
	return !delivered, nil
}

func (s *Service) recordDefaultReportDelivery(ctx context.Context, period string, from, to time.Time) error {
	if period == "daily" {
		return nil
	}
	return s.analytics.RecordReportDelivery(ctx, period, from, to)
}

func DefaultRange(period string, now time.Time) (time.Time, time.Time) {
	today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	switch period {
	case "daily":
		return today.AddDate(0, 0, -1), today
	case "weekly":
		daysSinceMonday := (int(today.Weekday()) + 6) % 7
		thisWeekStart := today.AddDate(0, 0, -daysSinceMonday)
		return thisWeekStart.AddDate(0, 0, -7), thisWeekStart
	case "monthly":
		thisMonthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
		return thisMonthStart.AddDate(0, -1, 0), thisMonthStart
	default:
		return today.AddDate(0, 0, -30), today
	}
}

func previousReportRange(period string, from, to time.Time) (time.Time, time.Time) {
	switch period {
	case "daily":
		return from.AddDate(0, 0, -1), from
	case "weekly":
		return from.AddDate(0, 0, -7), from
	case "monthly":
		return from.AddDate(0, -1, 0), from
	default:
		return from.Add(-to.Sub(from)), from
	}
}

func topCostMovements(variances []domain.CostVariance, limit int) ([]domain.CostVariance, []domain.CostVariance) {
	increases := make([]domain.CostVariance, 0, limit)
	decreases := make([]domain.CostVariance, 0, limit)
	for _, variance := range variances {
		switch {
		case variance.Direction == "increase" && variance.Delta > 0:
			increases = append(increases, variance)
		case variance.Direction == "decrease" && variance.Delta < 0:
			decreases = append(decreases, variance)
		}
	}
	sort.SliceStable(increases, func(i, j int) bool {
		return math.Abs(increases[i].Delta) > math.Abs(increases[j].Delta)
	})
	sort.SliceStable(decreases, func(i, j int) bool {
		return math.Abs(decreases[i].Delta) > math.Abs(decreases[j].Delta)
	})
	if len(increases) > limit {
		increases = increases[:limit]
	}
	if len(decreases) > limit {
		decreases = decreases[:limit]
	}
	return increases, decreases
}
