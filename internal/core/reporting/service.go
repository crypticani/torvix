package reporting

import (
	"context"
	"time"

	"github.com/crypticani/cloudpulse/internal/core/analytics"
	"github.com/crypticani/cloudpulse/internal/core/forecasting"
	"github.com/crypticani/cloudpulse/internal/domain"
)

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
		Generated: time.Now().UTC(),
		Summary:   summary,
		Anomalies: anomalies,
		Forecast:  forecast,
	}, nil
}
