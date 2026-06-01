package collect

import (
	"context"
	"testing"
	"time"

	"github.com/crypticani/torvix/internal/core/normalize"
	"github.com/crypticani/torvix/internal/domain"
	"github.com/crypticani/torvix/internal/ports/providers"
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

func TestRunInsertsRecentMay2026OCIRecordAndRefreshesDashboardSummary(t *testing.T) {
	recentRecord := rawCostRecord(time.Date(2026, 5, 17, 2, 30, 0, 0, time.UTC), 12)
	repo := &recordFilteringRepo{}
	svc := NewWithPolicy(nil, repo, normalize.New(), []providers.Collector{
		&batchCollector{result: providers.CollectResult{
			FilesProcessed:   1,
			RecordsProcessed: 1,
			Batches: []providers.FileBatch{{
				Metadata: processedFile("reports/cost-csv/0001000000670898.csv.gz"),
				Records:  []domain.RawBillingRecord{recentRecord},
			}},
		}},
	}, nil, Policy{LookbackDays: 30, RetentionDays: 90, CompressionAfterDays: 7})

	results, err := svc.Run(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if results[0].RecordsWithinLookback != 1 || results[0].RecordsInserted != 1 {
		t.Fatalf("expected recent May 2026 record to be inserted, got %+v", results[0])
	}
	summaries, err := repo.DashboardCostSummaries(context.Background(), "daily", time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("DashboardCostSummaries() error = %v", err)
	}
	if len(summaries) == 0 {
		t.Fatalf("expected dashboard summary for inserted May 2026 record")
	}
	if summaries[0].PeriodStart != time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC) || summaries[0].TotalCost != 12 {
		t.Fatalf("unexpected summary: %+v", summaries[0])
	}
}

func TestRunSkipsGenuinelyOldOCI2022Record(t *testing.T) {
	oldRecord := rawCostRecord(time.Date(2022, 5, 17, 2, 30, 0, 0, time.UTC), 12)
	repo := &recordFilteringRepo{}
	svc := NewWithPolicy(nil, repo, normalize.New(), []providers.Collector{
		&batchCollector{result: providers.CollectResult{
			FilesProcessed:   1,
			RecordsProcessed: 1,
			Batches: []providers.FileBatch{{
				Metadata: processedFile("reports/cost-csv/0001000000999999.csv.gz"),
				Records:  []domain.RawBillingRecord{oldRecord},
			}},
		}},
	}, nil, Policy{LookbackDays: 30, RetentionDays: 90, CompressionAfterDays: 7})

	results, err := svc.Run(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if results[0].RecordsWithinLookback != 0 || results[0].RecordsSkippedOld != 1 || results[0].RecordsInserted != 0 {
		t.Fatalf("expected old 2022 record to be skipped, got %+v", results[0])
	}
	summaries, err := repo.DashboardCostSummaries(context.Background(), "daily", time.Date(2022, 5, 1, 0, 0, 0, 0, time.UTC), time.Date(2022, 6, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("DashboardCostSummaries() error = %v", err)
	}
	if len(summaries) != 0 {
		t.Fatalf("expected no dashboard summaries for skipped old record, got %+v", summaries)
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
	ingestedBatches    [][]domain.CanonicalCostRecord
	dashboardSummaries []domain.DashboardCostSummary
	lifecycle          domain.DataLifecycleMaintenance
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
func (r *recordFilteringRepo) IsReportDelivered(context.Context, domain.ReportDeliveryKey) (bool, error) {
	return false, nil
}
func (r *recordFilteringRepo) RecordReportDelivery(context.Context, domain.ReportDeliveryKey) error {
	return nil
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
	totals := map[time.Time]float64{}
	for _, batch := range r.ingestedBatches {
		for _, record := range batch {
			day := record.Timestamp.UTC().Truncate(24 * time.Hour)
			totals[day] += record.Cost
		}
	}
	r.dashboardSummaries = r.dashboardSummaries[:0]
	for day, total := range totals {
		r.dashboardSummaries = append(r.dashboardSummaries, domain.DashboardCostSummary{
			PeriodStart: day,
			PeriodEnd:   day.Add(24 * time.Hour),
			Provider:    domain.ProviderOCI,
			TotalCost:   total,
			UpdatedAt:   time.Now().UTC(),
		})
	}
	return nil
}
func (r *recordFilteringRepo) DashboardOverview(context.Context, time.Time, time.Time, time.Time, time.Time) (domain.DashboardOverview, error) {
	return domain.DashboardOverview{}, nil
}
func (r *recordFilteringRepo) DashboardCostSummaries(_ context.Context, _ string, from, to time.Time) ([]domain.DashboardCostSummary, error) {
	var out []domain.DashboardCostSummary
	for _, summary := range r.dashboardSummaries {
		if summary.PeriodStart.Before(from) || !summary.PeriodStart.Before(to) {
			continue
		}
		out = append(out, summary)
	}
	return out, nil
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
