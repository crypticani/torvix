package analytics

import (
	"context"
	"math"
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
	return s.repo.AggregateCosts(ctx, from, to, "daily")
}

func (s *Service) AggregateWindow(ctx context.Context, from, to time.Time, window string) ([]domain.AggregatedCost, error) {
	return s.repo.AggregateCosts(ctx, from, to, window)
}

func (s *Service) DetectAnomalies(ctx context.Context, from, to time.Time) ([]domain.Anomaly, error) {
	return s.repo.DetectAnomalies(ctx, from, to)
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
