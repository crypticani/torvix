package storage

import (
	"context"
	"time"

	"github.com/crypticani/cloudpulse/internal/domain"
)

type Repository interface {
	StoreIngestedBatch(ctx context.Context, file domain.ProcessedReportFile, records []domain.CanonicalCostRecord) error
	StoreCostRecords(ctx context.Context, records []domain.CanonicalCostRecord) error
	DeleteCostRecordsForSource(ctx context.Context, provider domain.Provider, sourceObject string) error
	MarkReportProcessed(ctx context.Context, file domain.ProcessedReportFile) error
	AggregateCosts(ctx context.Context, from, to time.Time, window string) ([]domain.AggregatedCost, error)
	DetectAnomalies(ctx context.Context, from, to time.Time) ([]domain.Anomaly, error)
	ForecastCosts(ctx context.Context, from, to time.Time, horizon int) ([]domain.ForecastPoint, error)
	IsReportProcessed(ctx context.Context, provider domain.Provider, bucket, objectName, etag string) (bool, error)
	RefreshAggregates(ctx context.Context, from, to time.Time) error
}
