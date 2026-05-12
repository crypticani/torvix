package analytics

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/crypticani/cloudpulse/internal/domain"
	"github.com/crypticani/cloudpulse/internal/ports/storage"
)

type Service struct {
	repo storage.Repository
}

func New(repo storage.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Aggregate(ctx context.Context, from, to time.Time) ([]domain.AggregatedCost, error) {
	return s.repo.GetDailyCostsByService(ctx, from, to)
}

func (s *Service) AggregateWindow(ctx context.Context, from, to time.Time, window string) ([]domain.AggregatedCost, error) {
	points, err := s.repo.GetDailyCostsByService(ctx, from, to)
	if err != nil {
		return nil, err
	}
	if window == "" || window == "daily" {
		return points, nil
	}

	type key struct {
		windowStart time.Time
		windowEnd   time.Time
		provider    domain.Provider
		accountID   string
		service     string
	}

	buckets := map[key]float64{}
	for _, p := range points {
		ws, we := aggregateBounds(p.WindowStart, window)
		k := key{
			windowStart: ws,
			windowEnd:   we,
			provider:    p.Provider,
			accountID:   p.AccountID,
			service:     p.Service,
		}
		buckets[k] += p.TotalCost
	}

	out := make([]domain.AggregatedCost, 0, len(buckets))
	for k, total := range buckets {
		out = append(out, domain.AggregatedCost{
			WindowStart: k.windowStart,
			WindowEnd:   k.windowEnd,
			Provider:    k.provider,
			AccountID:   k.accountID,
			Service:     k.service,
			TotalCost:   total,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].WindowStart.Before(out[j].WindowStart)
	})
	return out, nil
}

func (s *Service) DetectAnomalies(ctx context.Context, from, to time.Time) ([]domain.Anomaly, error) {
	points, err := s.repo.GetDailyCostsByService(ctx, from, to)
	if err != nil {
		return nil, err
	}

	grouped := map[string][]domain.AggregatedCost{}
	for _, p := range points {
		key := string(p.Provider) + "|" + p.AccountID + "|" + p.Service
		grouped[key] = append(grouped[key], p)
	}

	var anomalies []domain.Anomaly
	for _, series := range grouped {
		sort.Slice(series, func(i, j int) bool {
			return series[i].WindowStart.Before(series[j].WindowStart)
		})
		anomalies = append(anomalies, detectSeriesAnomalies(series)...)
	}

	if err := s.repo.SaveAnomalies(ctx, anomalies); err != nil {
		return nil, err
	}
	return anomalies, nil
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

func aggregateBounds(t time.Time, window string) (time.Time, time.Time) {
	t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	switch window {
	case "weekly":
		offset := (int(t.Weekday()) + 6) % 7
		start := t.AddDate(0, 0, -offset)
		return start, start.AddDate(0, 0, 6)
	case "monthly":
		start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 1, -1)
	default:
		return t, t
	}
}
