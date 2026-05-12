package forecasting

import (
	"context"
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

func (s *Service) Forecast(ctx context.Context, from, to time.Time, horizon int) ([]domain.ForecastPoint, error) {
	points, err := s.repo.GetDailyCostsByService(ctx, from, to)
	if err != nil {
		return nil, err
	}
	grouped := map[string][]domain.AggregatedCost{}
	for _, p := range points {
		key := string(p.Provider) + "|" + p.AccountID + "|" + p.Service
		grouped[key] = append(grouped[key], p)
	}

	var forecasts []domain.ForecastPoint
	for _, series := range grouped {
		sort.Slice(series, func(i, j int) bool {
			return series[i].WindowStart.Before(series[j].WindowStart)
		})
		if len(series) == 0 {
			continue
		}
		sum := 0.0
		count := 0
		for i := max(0, len(series)-7); i < len(series); i++ {
			sum += series[i].TotalCost
			count++
		}
		avg := sum / float64(count)
		last := series[len(series)-1]
		for d := 1; d <= horizon; d++ {
			forecasts = append(forecasts, domain.ForecastPoint{
				Date:           last.WindowStart.AddDate(0, 0, d),
				Provider:       last.Provider,
				AccountID:      last.AccountID,
				Service:        last.Service,
				ForecastCost:   avg,
				ConfidenceLow:  avg * 0.9,
				ConfidenceHigh: avg * 1.1,
			})
		}
	}

	return forecasts, nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
