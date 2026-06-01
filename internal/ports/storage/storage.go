package storage

import (
	"context"
	"time"

	"github.com/crypticani/torvix/internal/domain"
)

type Repository interface {
	StoreIngestedBatch(ctx context.Context, file domain.ProcessedReportFile, records []domain.CanonicalCostRecord) error
	StoreCostRecords(ctx context.Context, records []domain.CanonicalCostRecord) error
	DeleteCostRecordsForSource(ctx context.Context, provider domain.Provider, sourceObject string) error
	MarkReportProcessed(ctx context.Context, file domain.ProcessedReportFile) error
	LastIngestionCheckpoint(ctx context.Context, provider domain.Provider) (time.Time, error)
	MarkIngestionCheckpoint(ctx context.Context, provider domain.Provider, checkpoint time.Time) error
	AggregateCosts(ctx context.Context, from, to time.Time, window string) ([]domain.AggregatedCost, error)
	CompareCostVariance(ctx context.Context, period string, currentFrom, currentTo, previousFrom, previousTo time.Time) ([]domain.CostVariance, error)
	DetectAnomalies(ctx context.Context, from, to time.Time) ([]domain.Anomaly, error)
	ForecastCosts(ctx context.Context, from, to time.Time, horizon int) ([]domain.ForecastPoint, error)
	IsReportProcessed(ctx context.Context, provider domain.Provider, bucket, objectName, etag string) (bool, error)
	IsReportDelivered(ctx context.Context, key domain.ReportDeliveryKey) (bool, error)
	RecordReportDelivery(ctx context.Context, key domain.ReportDeliveryKey) error
	ApplyDataLifecyclePolicies(ctx context.Context, retentionDays, compressionAfterDays int) error
	RunDataLifecycleMaintenance(ctx context.Context, retentionDays, compressionAfterDays int) (domain.DataLifecycleMaintenance, error)
	RefreshAggregates(ctx context.Context, from, to time.Time) error
	RefreshDashboardAnalytics(ctx context.Context, from, to time.Time, retentionDays int) error
	DashboardOverview(ctx context.Context, currentFrom, currentTo, previousFrom, previousTo time.Time) (domain.DashboardOverview, error)
	DashboardCostSummaries(ctx context.Context, window string, from, to time.Time) ([]domain.DashboardCostSummary, error)
	DashboardAnomalies(ctx context.Context, from, to time.Time, severity string) ([]domain.DashboardAnomaly, error)
	LatestIngestionStatus(ctx context.Context) (domain.IngestionStatusSummary, error)
}
