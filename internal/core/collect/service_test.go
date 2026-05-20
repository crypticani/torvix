package collect

import (
	"context"
	"testing"
	"time"

	"github.com/crypticani/cloudpulse/internal/core/normalize"
	"github.com/crypticani/cloudpulse/internal/domain"
	"github.com/crypticani/cloudpulse/internal/ports/providers"
)

func TestRunSkipsOldRecordsBeforeInsertion(t *testing.T) {
	now := time.Now().UTC()
	oldRecord := rawCostRecord(now.AddDate(0, 0, -45), 10)
	repo := &recordFilteringRepo{}
	svc := NewWithPolicy(nil, repo, normalize.New(), []providers.Collector{
		&batchCollector{result: providers.CollectResult{
			FilesProcessed:   1,
			RecordsProcessed: 1,
			Batches: []providers.FileBatch{{
				Metadata: processedFile("reports/old.csv"),
				Records:  []domain.RawBillingRecord{oldRecord},
			}},
		}},
	}, nil, Policy{LookbackDays: 30, RetentionDays: 90, CompressionAfterDays: 7})

	results, err := svc.Run(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(repo.ingestedBatches) != 1 {
		t.Fatalf("expected processed report to be marked with empty retained batch, got %d batches", len(repo.ingestedBatches))
	}
	if got := len(repo.ingestedBatches[0]); got != 0 {
		t.Fatalf("expected old record to be skipped before insertion, inserted %d", got)
	}
	if results[0].RecordsParsed != 1 || results[0].RecordsWithinLookback != 0 || results[0].RecordsSkippedOld != 1 || results[0].RecordsInserted != 0 {
		t.Fatalf("unexpected counters: %+v", results[0])
	}
	if repo.lifecycle.RecordsDeleted != 0 {
		t.Fatalf("retention should not need to delete lookback-skipped records, deleted %d", repo.lifecycle.RecordsDeleted)
	}
}

func TestRunInsertsRecentRecords(t *testing.T) {
	now := time.Now().UTC()
	recentRecord := rawCostRecord(now.AddDate(0, 0, -2), 12)
	repo := &recordFilteringRepo{}
	svc := NewWithPolicy(nil, repo, normalize.New(), []providers.Collector{
		&batchCollector{result: providers.CollectResult{
			FilesProcessed:   1,
			RecordsProcessed: 1,
			Batches: []providers.FileBatch{{
				Metadata: processedFile("reports/recent.csv"),
				Records:  []domain.RawBillingRecord{recentRecord},
			}},
		}},
	}, nil, Policy{LookbackDays: 30, RetentionDays: 90, CompressionAfterDays: 7})

	results, err := svc.Run(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(repo.ingestedBatches) != 1 || len(repo.ingestedBatches[0]) != 1 {
		t.Fatalf("expected one recent record inserted, batches=%v", repo.ingestedBatches)
	}
	if results[0].RecordsParsed != 1 || results[0].RecordsWithinLookback != 1 || results[0].RecordsSkippedOld != 0 || results[0].RecordsInserted != 1 {
		t.Fatalf("unexpected counters: %+v", results[0])
	}
}

func TestRunInsertsOnlyRecentRecordsFromMixedReport(t *testing.T) {
	now := time.Now().UTC()
	repo := &recordFilteringRepo{}
	svc := NewWithPolicy(nil, repo, normalize.New(), []providers.Collector{
		&batchCollector{result: providers.CollectResult{
			FilesProcessed:   1,
			RecordsProcessed: 2,
			Batches: []providers.FileBatch{{
				Metadata: processedFile("reports/mixed.csv"),
				Records: []domain.RawBillingRecord{
					rawCostRecord(now.AddDate(0, 0, -60), 10),
					rawCostRecord(now.AddDate(0, 0, -1), 20),
				},
			}},
		}},
	}, nil, Policy{LookbackDays: 30, RetentionDays: 90, CompressionAfterDays: 7})

	results, err := svc.Run(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(repo.ingestedBatches) != 1 || len(repo.ingestedBatches[0]) != 1 {
		t.Fatalf("expected only recent record inserted, batches=%v", repo.ingestedBatches)
	}
	if repo.ingestedBatches[0][0].Cost != 20 {
		t.Fatalf("expected recent record cost 20, got %f", repo.ingestedBatches[0][0].Cost)
	}
	if results[0].RecordsParsed != 2 || results[0].RecordsWithinLookback != 1 || results[0].RecordsSkippedOld != 1 || results[0].RecordsInserted != 1 {
		t.Fatalf("unexpected counters: %+v", results[0])
	}
}

type batchCollector struct {
	result providers.CollectResult
}

func (c *batchCollector) Name() string { return "oci" }

func (c *batchCollector) Collect(context.Context, time.Time) (providers.CollectResult, error) {
	return c.result, nil
}

type recordFilteringRepo struct {
	ingestedBatches [][]domain.CanonicalCostRecord
	lifecycle       domain.DataLifecycleMaintenance
}

func (r *recordFilteringRepo) StoreIngestedBatch(_ context.Context, _ domain.ProcessedReportFile, records []domain.CanonicalCostRecord) error {
	r.ingestedBatches = append(r.ingestedBatches, append([]domain.CanonicalCostRecord(nil), records...))
	return nil
}
func (r *recordFilteringRepo) StoreCostRecords(_ context.Context, records []domain.CanonicalCostRecord) error {
	r.ingestedBatches = append(r.ingestedBatches, append([]domain.CanonicalCostRecord(nil), records...))
	return nil
}
func (r *recordFilteringRepo) DeleteCostRecordsForSource(context.Context, domain.Provider, string) error {
	return nil
}
func (r *recordFilteringRepo) MarkReportProcessed(context.Context, domain.ProcessedReportFile) error {
	return nil
}
func (r *recordFilteringRepo) LastIngestionCheckpoint(context.Context, domain.Provider) (time.Time, error) {
	return time.Time{}, nil
}
func (r *recordFilteringRepo) MarkIngestionCheckpoint(context.Context, domain.Provider, time.Time) error {
	return nil
}
func (r *recordFilteringRepo) AggregateCosts(context.Context, time.Time, time.Time, string) ([]domain.AggregatedCost, error) {
	return nil, nil
}
func (r *recordFilteringRepo) CompareCostVariance(context.Context, string, time.Time, time.Time, time.Time, time.Time) ([]domain.CostVariance, error) {
	return nil, nil
}
func (r *recordFilteringRepo) DetectAnomalies(context.Context, time.Time, time.Time) ([]domain.Anomaly, error) {
	return nil, nil
}
func (r *recordFilteringRepo) ForecastCosts(context.Context, time.Time, time.Time, int) ([]domain.ForecastPoint, error) {
	return nil, nil
}
func (r *recordFilteringRepo) IsReportProcessed(context.Context, domain.Provider, string, string, string) (bool, error) {
	return false, nil
}
func (r *recordFilteringRepo) ApplyDataLifecyclePolicies(context.Context, int, int) error {
	return nil
}
func (r *recordFilteringRepo) RunDataLifecycleMaintenance(context.Context, int, int) (domain.DataLifecycleMaintenance, error) {
	return r.lifecycle, nil
}
func (r *recordFilteringRepo) RefreshAggregates(context.Context, time.Time, time.Time) error {
	return nil
}
func (r *recordFilteringRepo) RefreshDashboardAnalytics(context.Context, time.Time, time.Time, int) error {
	return nil
}
func (r *recordFilteringRepo) DashboardOverview(context.Context, time.Time, time.Time, time.Time, time.Time) (domain.DashboardOverview, error) {
	return domain.DashboardOverview{}, nil
}
func (r *recordFilteringRepo) DashboardCostSummaries(context.Context, string, time.Time, time.Time) ([]domain.DashboardCostSummary, error) {
	return nil, nil
}
func (r *recordFilteringRepo) DashboardAnomalies(context.Context, time.Time, time.Time, string) ([]domain.DashboardAnomaly, error) {
	return nil, nil
}
func (r *recordFilteringRepo) LatestIngestionStatus(context.Context) (domain.IngestionStatusSummary, error) {
	return domain.IngestionStatusSummary{}, nil
}

func rawCostRecord(ts time.Time, cost float64) domain.RawBillingRecord {
	return domain.RawBillingRecord{
		Provider:    domain.ProviderOCI,
		AccountID:   "acct",
		UsageStart:  ts,
		Service:     "Compute",
		Category:    "compute",
		Region:      "us-ashburn-1",
		Currency:    "USD",
		Cost:        cost,
		UsageAmount: 1,
		UsageUnit:   "hour",
	}
}

func processedFile(name string) domain.ProcessedReportFile {
	return domain.ProcessedReportFile{
		Provider:     domain.ProviderOCI,
		Bucket:       "bucket",
		ObjectName:   name,
		ETag:         "etag",
		LastModified: time.Now().UTC(),
	}
}
