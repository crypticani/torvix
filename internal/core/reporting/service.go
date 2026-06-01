package reporting

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/crypticani/torvix/internal/core/analytics"
	"github.com/crypticani/torvix/internal/core/forecasting"
	"github.com/crypticani/torvix/internal/domain"
)

var DefaultReportPeriods = []string{"daily", "weekly", "monthly"}

const (
	maxCostMovements = 5
	defaultProvider  = "all"

	SkipDailyIncomplete  = "Skipping daily report because target date data is incomplete."
	SkipWeeklyIncomplete = "Skipping weekly report because report range data is incomplete."
)

type ReportSender interface {
	SendReport(ctx context.Context, report domain.Report) error
}

type DestinationReportSender interface {
	ReportSender
	ReportDestinations() []string
	SendReportToDestination(ctx context.Context, destination string, report domain.Report) error
}

type Options struct {
	Timezone                 string
	Location                 *time.Location
	DailyReportTargetLagDays int
	RequireCompleteIngestion bool
}

type DeliverOptions struct {
	Force bool
}

type DeliveryResult struct {
	Period      string
	From        time.Time
	To          time.Time
	Destination string
	Skipped     bool
	SkipReason  string
	Error       error
}

type Service struct {
	analytics                *analytics.Service
	forecasting              *forecasting.Service
	location                 *time.Location
	dailyReportTargetLagDays int
	requireCompleteIngestion bool
}

func New(a *analytics.Service, f *forecasting.Service) *Service {
	return NewWithOptions(a, f, Options{})
}

func NewWithOptions(a *analytics.Service, f *forecasting.Service, opts Options) *Service {
	location := opts.Location
	if location == nil && opts.Timezone != "" {
		if loaded, err := time.LoadLocation(opts.Timezone); err == nil {
			location = loaded
		}
	}
	if location == nil {
		location = time.UTC
	}
	lagDays := opts.DailyReportTargetLagDays
	if lagDays <= 0 {
		lagDays = 1
	}
	return &Service{
		analytics:                a,
		forecasting:              f,
		location:                 location,
		dailyReportTargetLagDays: lagDays,
		requireCompleteIngestion: opts.RequireCompleteIngestion,
	}
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
	from, to := s.defaultRange(period, now)
	summary, err := s.analytics.AggregateWindow(ctx, from, to, period)
	if err != nil {
		return domain.Report{}, err
	}
	return s.buildWithSummary(ctx, period, from, to, summary)
}

func (s *Service) DeliverDefaultReports(ctx context.Context, sender ReportSender, now time.Time) []DeliveryResult {
	return s.DeliverDefaultReportsWithOptions(ctx, sender, now, DeliverOptions{})
}

func (s *Service) DeliverDefaultReportsWithOptions(ctx context.Context, sender ReportSender, now time.Time, opts DeliverOptions) []DeliveryResult {
	results := make([]DeliveryResult, 0, len(DefaultReportPeriods))
	if sender == nil {
		for _, period := range DefaultReportPeriods {
			from, to := s.defaultRange(period, now)
			results = append(results, DeliveryResult{Period: period, From: from, To: to, Error: fmt.Errorf("report sender is not configured")})
		}
		return results
	}
	for _, period := range DefaultReportPeriods {
		results = append(results, s.DeliverScheduledReport(ctx, sender, period, now, opts)...)
	}
	return results
}

func (s *Service) DeliverScheduledReport(ctx context.Context, sender ReportSender, period string, now time.Time, opts DeliverOptions) []DeliveryResult {
	from, to := s.defaultRange(period, now)
	resultBase := DeliveryResult{Period: period, From: from, To: to}
	if sender == nil {
		resultBase.Error = fmt.Errorf("report sender is not configured")
		return []DeliveryResult{resultBase}
	}
	if skipReason, err := s.incompleteSkipReason(ctx, period, from, to); err != nil {
		resultBase.Error = err
		return []DeliveryResult{resultBase}
	} else if skipReason != "" {
		resultBase.Skipped = true
		resultBase.SkipReason = skipReason
		return []DeliveryResult{resultBase}
	}

	destinations := reportDestinations(sender)
	results := make([]DeliveryResult, 0, len(destinations))
	pendingDestinations := make([]string, 0, len(destinations))
	for _, destination := range destinations {
		result := resultBase
		result.Destination = destination
		key := reportDeliveryKey(period, from, to, destination)
		deliver, err := s.shouldDeliverReport(ctx, key, opts.Force)
		if err != nil {
			result.Error = err
			results = append(results, result)
			continue
		}
		if !deliver {
			result.Skipped = true
			result.SkipReason = "report already delivered"
			results = append(results, result)
			continue
		}
		pendingDestinations = append(pendingDestinations, destination)
	}
	if len(pendingDestinations) == 0 {
		return results
	}

	report, err := s.BuildDefault(ctx, period, now)
	if err != nil {
		resultBase.Error = err
		for _, destination := range pendingDestinations {
			result := resultBase
			result.Destination = destination
			results = append(results, result)
		}
		return results
	}
	results = append(results, s.deliverReportToDestinations(ctx, sender, report, pendingDestinations)...)
	return results
}

func (s *Service) DeliverReport(ctx context.Context, sender ReportSender, report domain.Report, opts DeliverOptions) []DeliveryResult {
	resultBase := DeliveryResult{Period: report.Period, From: report.From, To: report.To}
	if sender == nil {
		resultBase.Error = fmt.Errorf("report sender is not configured")
		return []DeliveryResult{resultBase}
	}
	destinations := reportDestinations(sender)
	if opts.Force {
		return s.deliverReportToDestinations(ctx, sender, report, destinations)
	}
	results := make([]DeliveryResult, 0, len(destinations))
	pendingDestinations := make([]string, 0, len(destinations))
	for _, destination := range destinations {
		result := resultBase
		result.Destination = destination
		key := reportDeliveryKey(report.Period, report.From, report.To, destination)
		deliver, err := s.shouldDeliverReport(ctx, key, opts.Force)
		if err != nil {
			result.Error = err
			results = append(results, result)
			continue
		}
		if !deliver {
			result.Skipped = true
			result.SkipReason = "report already delivered"
			results = append(results, result)
			continue
		}
		pendingDestinations = append(pendingDestinations, destination)
	}
	results = append(results, s.deliverReportToDestinations(ctx, sender, report, pendingDestinations)...)
	return results
}

func (s *Service) deliverReportToDestinations(ctx context.Context, sender ReportSender, report domain.Report, destinations []string) []DeliveryResult {
	results := make([]DeliveryResult, 0, len(destinations))
	for _, destination := range destinations {
		result := DeliveryResult{Period: report.Period, From: report.From, To: report.To, Destination: destination}
		result.From = report.From
		result.To = report.To
		if err := sendReportToDestination(ctx, sender, destination, report); err != nil {
			result.Error = err
			results = append(results, result)
			continue
		}
		if err := s.analytics.RecordReportDelivery(ctx, reportDeliveryKey(report.Period, report.From, report.To, destination)); err != nil {
			result.Error = err
		}
		results = append(results, result)
	}
	return results
}

func (s *Service) shouldDeliverReport(ctx context.Context, key domain.ReportDeliveryKey, force bool) (bool, error) {
	if force {
		return true, nil
	}
	delivered, err := s.analytics.IsReportDelivered(ctx, key)
	if err != nil {
		return false, err
	}
	return !delivered, nil
}

func (s *Service) incompleteSkipReason(ctx context.Context, period string, from, to time.Time) (string, error) {
	if !s.requireCompleteIngestion {
		return "", nil
	}
	switch period {
	case "daily":
		complete, err := s.rangeHasDailyData(ctx, from, to)
		if err != nil {
			return "", err
		}
		if !complete {
			return SkipDailyIncomplete, nil
		}
	case "weekly":
		complete, err := s.rangeHasDailyData(ctx, from, to)
		if err != nil {
			return "", err
		}
		if !complete {
			return SkipWeeklyIncomplete, nil
		}
	}
	return "", nil
}

func (s *Service) rangeHasDailyData(ctx context.Context, from, to time.Time) (bool, error) {
	dayStart := localDayStart(from.In(s.location))
	end := localDayStart(to.In(s.location))
	for dayStart.Before(end) {
		dayEnd := dayStart.AddDate(0, 0, 1)
		rows, err := s.analytics.AggregateWindow(ctx, dayStart.UTC(), dayEnd.UTC(), "daily")
		if err != nil {
			return false, err
		}
		if len(rows) == 0 {
			return false, nil
		}
		dayStart = dayEnd
	}
	return true, nil
}

func reportDestinations(sender ReportSender) []string {
	if destinationSender, ok := sender.(DestinationReportSender); ok {
		destinations := destinationSender.ReportDestinations()
		if len(destinations) > 0 {
			return destinations
		}
	}
	return []string{"default"}
}

func sendReportToDestination(ctx context.Context, sender ReportSender, destination string, report domain.Report) error {
	if destinationSender, ok := sender.(DestinationReportSender); ok && destination != "default" {
		return destinationSender.SendReportToDestination(ctx, destination, report)
	}
	return sender.SendReport(ctx, report)
}

func reportDeliveryKey(period string, from, to time.Time, destination string) domain.ReportDeliveryKey {
	return domain.ReportDeliveryKey{
		Provider:    defaultProvider,
		ReportType:  period,
		PeriodStart: from,
		PeriodEnd:   to,
		Destination: destination,
	}
}

func (s *Service) defaultRange(period string, now time.Time) (time.Time, time.Time) {
	return DefaultRangeWithOptions(period, now, Options{
		Location:                 s.location,
		DailyReportTargetLagDays: s.dailyReportTargetLagDays,
	})
}

func DefaultRange(period string, now time.Time) (time.Time, time.Time) {
	return DefaultRangeWithOptions(period, now, Options{Location: time.UTC, DailyReportTargetLagDays: 1})
}

func DefaultRangeWithOptions(period string, now time.Time, opts Options) (time.Time, time.Time) {
	location := opts.Location
	if location == nil {
		location = time.UTC
	}
	lagDays := opts.DailyReportTargetLagDays
	if lagDays <= 0 {
		lagDays = 1
	}
	today := localDayStart(now.In(location))
	switch period {
	case "daily":
		return today.AddDate(0, 0, -lagDays).UTC(), today.AddDate(0, 0, -(lagDays - 1)).UTC()
	case "weekly":
		daysSinceMonday := (int(today.Weekday()) + 6) % 7
		thisWeekStart := today.AddDate(0, 0, -daysSinceMonday)
		return thisWeekStart.AddDate(0, 0, -7).UTC(), thisWeekStart.UTC()
	case "monthly":
		thisMonthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, location)
		return thisMonthStart.AddDate(0, -1, 0).UTC(), thisMonthStart.UTC()
	default:
		return today.AddDate(0, 0, -30).UTC(), today.UTC()
	}
}

func localDayStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
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
