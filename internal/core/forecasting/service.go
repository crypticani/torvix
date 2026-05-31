package forecasting

import (
	"context"
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

func (s *Service) Forecast(ctx context.Context, from, to time.Time, horizon int) ([]domain.ForecastPoint, error) {
	return s.repo.ForecastCosts(ctx, from, to, horizon)
}
