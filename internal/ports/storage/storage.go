package storage

import (
	"context"
	"time"

	"github.com/crypticani/cloudpulse/internal/domain"
)

type Repository interface {
	InsertCanonical(ctx context.Context, records []domain.CanonicalCostRecord) error
	GetDailyCosts(ctx context.Context, from, to time.Time) ([]domain.AggregatedCost, error)
	GetDailyCostsByService(ctx context.Context, from, to time.Time) ([]domain.AggregatedCost, error)
	SaveAnomalies(ctx context.Context, anomalies []domain.Anomaly) error
	GetAnomalies(ctx context.Context, from, to time.Time) ([]domain.Anomaly, error)
}
