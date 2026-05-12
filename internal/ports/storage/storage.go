package storage

import (
	"context"
	"time"

	"github.com/crypticani/cloudpulse/internal/domain"
)

type Repository interface {
	StoreIngestedBatch(ctx context.Context, file domain.ProcessedReportFile, records []domain.CanonicalCostRecord) error
	AggregateCosts(ctx context.Context, from, to time.Time, window string) ([]domain.AggregatedCost, error)
	DetectAnomalies(ctx context.Context, from, to time.Time) ([]domain.Anomaly, error)
	ForecastCosts(ctx context.Context, from, to time.Time, horizon int) ([]domain.ForecastPoint, error)
	IsReportProcessed(ctx context.Context, provider domain.Provider, bucket, objectName, etag string) (bool, error)
}
