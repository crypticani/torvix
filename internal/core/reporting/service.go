package reporting

import (
	"context"
	"fmt"
	"time"

	"github.com/crypticani/cloudpulse/internal/core/analytics"
	"github.com/crypticani/cloudpulse/internal/core/forecasting"
	"github.com/crypticani/cloudpulse/internal/domain"
)

var DefaultReportPeriods = []string{"daily", "weekly", "monthly"}

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
	return domain.Report{
		Period:    period,
		From:      from,
		To:        to,
		Generated: time.Now().UTC(),
		Summary:   summary,
		Anomalies: anomalies,
		Forecast:  forecast,
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
		report, err := s.BuildDefault(ctx, period, now)
		if err != nil {
			result.Error = err
			results = append(results, result)
			continue
		}
		if len(report.Summary) > 0 {
			result.From = report.Summary[0].WindowStart
			result.To = report.Summary[0].WindowEnd
		}
		if err := sender.SendReport(ctx, report); err != nil {
			result.Error = err
		}
		results = append(results, result)
	}
	return results
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
