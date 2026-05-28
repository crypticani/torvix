package oci

import (
	"context"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/crypticani/cloudpulse/internal/config"
	"github.com/crypticani/cloudpulse/internal/domain"
	"github.com/crypticani/cloudpulse/internal/ports/providers"
)

func TestCollectorSkipsProcessedFiles(t *testing.T) {
	repo := &fakeRepository{
		processed: map[string]bool{
			"reports/older.csv|etag-1": true,
		},
	}
	client := &fakeObjectStorageClient{
		namespace: "bling",
		objects: []ObjectInfo{
			{Name: "reports/older.csv", ETag: "etag-1", LastModified: time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)},
			{Name: "reports/newer.csv", ETag: "etag-2", LastModified: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)},
		},
		bodies: map[string]string{
			"reports/newer.csv": strings.Join([]string{
				"lineItem/intervalUsageStart,product/service,usage/billedQuantity,cost/myCost",
				"2026-05-03T00:00:00Z,Compute,1,3.5",
			}, "\n"),
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	collector := NewWithClient(config.Provider{
		Bucket:  "tenant-bucket",
		Prefix:  "reports/",
		Account: "ocid1.tenancy.oc1..example",
	}, logger, repo, client)

	result, err := collector.Collect(context.Background(), time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if result.FilesSkipped != 1 {
		t.Fatalf("expected 1 skipped file, got %d", result.FilesSkipped)
	}
	if result.FilesProcessed != 1 {
		t.Fatalf("expected 1 processed file, got %d", result.FilesProcessed)
	}
	if result.RecordsProcessed != 1 {
		t.Fatalf("expected 1 processed record, got %d", result.RecordsProcessed)
	}
}

func TestCollectorCollectStreamFlushesBoundedBatches(t *testing.T) {
	repo := &fakeRepository{processed: map[string]bool{}}
	client := &fakeObjectStorageClient{
		namespace: "bling",
		objects: []ObjectInfo{
			{Name: "reports/newer.csv", ETag: "etag-2", LastModified: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)},
		},
		bodies: map[string]string{
			"reports/newer.csv": strings.Join([]string{
				"lineItem/intervalUsageStart,product/service,usage/billedQuantity,cost/myCost",
				"2026-05-03T00:00:00Z,Compute,1,3.5",
				"2026-05-03T01:00:00Z,Compute,2,4.5",
				"2026-05-03T02:00:00Z,Compute,3,5.5",
			}, "\n"),
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	collector := NewWithClient(config.Provider{
		Bucket:                 "tenant-bucket",
		Prefix:                 "reports/",
		Account:                "ocid1.tenancy.oc1..example",
		MaxFilesPerRun:         5,
		MaxRecordsPerBatch:     2,
		MaxMemoryBufferRecords: 2,
		MaxRuntime:             "1m",
	}, logger, repo, client)

	var batchSizes []int
	finalSeen := false
	result, err := collector.CollectStream(context.Background(), time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), func(_ context.Context, batch providers.FileBatch) error {
		if batch.Final {
			finalSeen = true
			if batch.Metadata.RecordCount != 3 {
				t.Fatalf("expected final record count 3, got %d", batch.Metadata.RecordCount)
			}
			return nil
		}
		if len(batch.Records) > 2 {
			t.Fatalf("batch exceeded configured limit: %d", len(batch.Records))
		}
		batchSizes = append(batchSizes, len(batch.Records))
		return nil
	})
	if err != nil {
		t.Fatalf("CollectStream() error = %v", err)
	}
	if !finalSeen {
		t.Fatalf("expected final processed marker callback")
	}
	if result.RecordsProcessed != 3 {
		t.Fatalf("expected 3 records processed, got %d", result.RecordsProcessed)
	}
	if len(batchSizes) != 2 || batchSizes[0] != 2 || batchSizes[1] != 1 {
		t.Fatalf("unexpected batch sizes: %v", batchSizes)
	}
}

func TestCollectorUsesProcessedReportsInsteadOfLastModifiedForDedupe(t *testing.T) {
	repo := &fakeRepository{processed: map[string]bool{}}
	client := &fakeObjectStorageClient{
		namespace: "bling",
		objects: []ObjectInfo{
			{Name: "reports/old.csv", ETag: "etag-old", LastModified: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)},
			{Name: "reports/new.csv", ETag: "etag-new", LastModified: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)},
		},
		bodies: map[string]string{
			"reports/old.csv": strings.Join([]string{
				"lineItem/intervalUsageStart,product/service,usage/billedQuantity,cost/myCost",
				"2026-04-01T00:00:00Z,Compute,1,2.5",
			}, "\n"),
			"reports/new.csv": strings.Join([]string{
				"lineItem/intervalUsageStart,product/service,usage/billedQuantity,cost/myCost",
				"2026-05-03T00:00:00Z,Compute,1,3.5",
			}, "\n"),
		},
		downloads: map[string]int{},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	collector := NewWithClient(config.Provider{
		Bucket:                 "tenant-bucket",
		Prefix:                 "reports/",
		Account:                "ocid1.tenancy.oc1..example",
		MaxFilesPerRun:         5,
		MaxRecordsPerBatch:     2,
		MaxMemoryBufferRecords: 2,
		MaxRuntime:             "1m",
	}, logger, repo, client)

	result, err := collector.CollectStream(context.Background(), time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), func(context.Context, providers.FileBatch) error {
		return nil
	})
	if err != nil {
		t.Fatalf("CollectStream() error = %v", err)
	}
	if result.SkippedOldFiles != 0 {
		t.Fatalf("expected no timestamp-based old-file skips, got %d", result.SkippedOldFiles)
	}
	if result.FilesProcessed != 2 {
		t.Fatalf("expected both unprocessed files to be processed, got %d", result.FilesProcessed)
	}
	if client.downloads["reports/old.csv"] != 1 {
		t.Fatalf("old unprocessed report should be downloaded once, got %d", client.downloads["reports/old.csv"])
	}
	if client.downloads["reports/new.csv"] != 1 {
		t.Fatalf("new report should be downloaded once, got %d", client.downloads["reports/new.csv"])
	}
}

func TestCollectorDoesNotTrustHigherNumericSuffixAsNewerBillingPeriod(t *testing.T) {
	repo := &fakeRepository{processed: map[string]bool{}}
	objects := []ObjectInfo{
		{Name: "reports/cost-csv/0001000000670898.csv.gz", ETag: "etag-recent"},
		{Name: "reports/cost-csv/0001000000999999.csv.gz", ETag: "etag-old"},
	}
	client := &fakeObjectStorageClient{
		namespace: "bling",
		objects:   objects,
		bodies: map[string]string{
			"reports/cost-csv/0001000000670898.csv.gz": costReportBody(t, "2026-05-17T02:30:00Z", 3.5),
			"reports/cost-csv/0001000000999999.csv.gz": costReportBody(t, "2022-05-17T02:30:00Z", 9.5),
		},
		downloads: map[string]int{},
	}
	collector := NewWithClient(config.Provider{
		Bucket:                 "tenant-bucket",
		Prefix:                 "reports/cost-csv/",
		Account:                "ocid1.tenancy.oc1..example",
		MaxFilesPerRun:         1,
		MaxRecordsPerBatch:     1,
		MaxMemoryBufferRecords: 1,
		MaxRuntime:             "1m",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, client)

	var selected []string
	result, err := collector.CollectStream(context.Background(), time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC), func(_ context.Context, batch providers.FileBatch) error {
		if !batch.Final && len(batch.Records) > 0 {
			selected = append(selected, batch.Metadata.ObjectName)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("CollectStream() error = %v", err)
	}
	if result.FilesProcessed != 1 {
		t.Fatalf("expected one file to be processed, got %d", result.FilesProcessed)
	}
	if len(selected) == 0 || selected[0] != "reports/cost-csv/0001000000670898.csv.gz" {
		t.Fatalf("expected list-order recent report first, got %v", selected)
	}
	if client.downloads["reports/cost-csv/0001000000999999.csv.gz"] != 0 {
		t.Fatalf("higher numeric old report should not be downloaded first")
	}
}

func TestCollectorSeeksRecentCostReportsWhenConfiguredWithBroadReportsPrefix(t *testing.T) {
	repo := &fakeRepository{processed: map[string]bool{}}
	objects := []ObjectInfo{
		{Name: "reports/cost-csv/0001000000641705.csv.gz", ETag: "etag-old-1", LastModified: time.Date(2022, 6, 9, 2, 39, 50, 0, time.UTC)},
		{Name: "reports/cost-csv/0001000001179402-00001.csv.gz", ETag: "etag-old-2", LastModified: time.Date(2023, 9, 23, 10, 46, 38, 0, time.UTC)},
		{Name: "reports/cost-csv/0001000002350019-00001.csv.gz", ETag: "etag-apr", LastModified: time.Date(2026, 4, 1, 3, 41, 9, 0, time.UTC)},
		{Name: "reports/cost-csv/0001000002400216-00001.csv.gz", ETag: "etag-may-1", LastModified: time.Date(2026, 5, 3, 8, 38, 13, 0, time.UTC)},
		{Name: "reports/cost-csv/0001000002420479-00001.csv.gz", ETag: "etag-may-2", LastModified: time.Date(2026, 5, 16, 0, 23, 58, 0, time.UTC)},
		{Name: "reports/usage-csv/0001000003000000.csv.gz", ETag: "etag-usage", LastModified: time.Date(2026, 5, 16, 0, 23, 58, 0, time.UTC)},
	}
	client := &fakeObjectStorageClient{
		namespace: "bling",
		objects:   objects,
		bodies: map[string]string{
			"reports/cost-csv/0001000000641705.csv.gz":       costReportBody(t, "2022-06-09T02:30:00Z", 1.5),
			"reports/cost-csv/0001000001179402-00001.csv.gz": costReportBody(t, "2023-09-23T10:30:00Z", 2.5),
			"reports/cost-csv/0001000002400216-00001.csv.gz": costReportBody(t, "2026-05-03T08:30:00Z", 3.5),
			"reports/cost-csv/0001000002420479-00001.csv.gz": costReportBody(t, "2026-05-16T00:00:00Z", 4.5),
		},
		downloads: map[string]int{},
	}
	collector := NewWithClient(config.Provider{
		Bucket:                 "tenant-bucket",
		Prefix:                 "reports/",
		Account:                "ocid1.tenancy.oc1..example",
		MaxObjectScan:          10,
		MaxFilesPerRun:         2,
		MaxRecordsPerBatch:     1,
		MaxMemoryBufferRecords: 1,
		MaxRuntime:             "1m",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, client)

	var selected []string
	result, err := collector.CollectStream(context.Background(), time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC), func(_ context.Context, batch providers.FileBatch) error {
		if !batch.Final && len(batch.Records) > 0 {
			selected = append(selected, batch.Metadata.ObjectName)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("CollectStream() error = %v", err)
	}
	if result.FilesProcessed != 2 {
		t.Fatalf("expected two recent cost reports to be processed, got %d", result.FilesProcessed)
	}
	want := []string{
		"reports/cost-csv/0001000002420479-00001.csv.gz",
		"reports/cost-csv/0001000002400216-00001.csv.gz",
	}
	if strings.Join(selected, ",") != strings.Join(want, ",") {
		t.Fatalf("expected recent cost reports newest-first %v, got %v", want, selected)
	}
	if client.downloads["reports/cost-csv/0001000000641705.csv.gz"] != 0 || client.downloads["reports/cost-csv/0001000001179402-00001.csv.gz"] != 0 {
		t.Fatalf("old reports should not be downloaded before recent candidates")
	}
	if client.downloads["reports/usage-csv/0001000003000000.csv.gz"] != 0 {
		t.Fatalf("usage reports should not be selected for cost ingestion")
	}
}

func TestCollectorStopsAfterConsecutiveZeroYieldReports(t *testing.T) {
	repo := &fakeRepository{processed: map[string]bool{}}
	objects := []ObjectInfo{
		{Name: "reports/cost-csv/0001000000999999.csv.gz", ETag: "etag-old-1"},
		{Name: "reports/cost-csv/0001000000999998.csv.gz", ETag: "etag-old-2"},
		{Name: "reports/cost-csv/0001000000670898.csv.gz", ETag: "etag-recent"},
	}
	client := &fakeObjectStorageClient{
		namespace: "bling",
		objects:   objects,
		bodies: map[string]string{
			"reports/cost-csv/0001000000999999.csv.gz": costReportBody(t, "2022-05-17T02:30:00Z", 9.5),
			"reports/cost-csv/0001000000999998.csv.gz": costReportBody(t, "2022-05-18T02:30:00Z", 8.5),
			"reports/cost-csv/0001000000670898.csv.gz": costReportBody(t, "2026-05-17T02:30:00Z", 3.5),
		},
		downloads: map[string]int{},
	}
	collector := NewWithClient(config.Provider{
		Bucket:                 "tenant-bucket",
		Prefix:                 "reports/cost-csv/",
		Account:                "ocid1.tenancy.oc1..example",
		MaxFilesPerRun:         10,
		MaxZeroYieldFiles:      2,
		MaxRecordsPerBatch:     1,
		MaxMemoryBufferRecords: 1,
		MaxRuntime:             "1m",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, client)

	var selected []string
	result, err := collector.CollectStream(context.Background(), time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC), func(_ context.Context, batch providers.FileBatch) error {
		if !batch.Final && len(batch.Records) > 0 {
			selected = append(selected, batch.Metadata.ObjectName)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("CollectStream() error = %v", err)
	}
	if result.FilesProcessed != 2 {
		t.Fatalf("expected scan to stop after two zero-yield files, got %d processed", result.FilesProcessed)
	}
	want := []string{
		"reports/cost-csv/0001000000999999.csv.gz",
		"reports/cost-csv/0001000000999998.csv.gz",
	}
	if strings.Join(selected, ",") != strings.Join(want, ",") {
		t.Fatalf("expected only zero-yield files before abort %v, got %v", want, selected)
	}
	if !result.HitZeroYieldLimit {
		t.Fatalf("expected zero-yield limit to be reported")
	}
	if client.downloads["reports/cost-csv/0001000000670898.csv.gz"] != 0 {
		t.Fatalf("recent file should not be downloaded after zero-yield abort")
	}
}

func TestCollectorSkipsProcessedNewestAndContinuesToNextNewestCostCsv(t *testing.T) {
	repo := &fakeRepository{
		processed: map[string]bool{
			"reports/cost-csv/0001000000999999.csv.gz|etag-3": true,
		},
	}
	objects := []ObjectInfo{
		{Name: "reports/cost-csv/0001000000999999.csv.gz", ETag: "etag-3"},
		{Name: "reports/cost-csv/0001000000670898.csv.gz", ETag: "etag-1"},
		{Name: "reports/cost-csv/0001000000676802.csv.gz", ETag: "etag-2"},
	}
	client := &fakeObjectStorageClient{
		namespace: "bling",
		objects:   objects,
		bodies:    costReportBodies(t, objects),
		downloads: map[string]int{},
	}
	collector := NewWithClient(config.Provider{
		Bucket:                 "tenant-bucket",
		Prefix:                 "reports/cost-csv/",
		Account:                "ocid1.tenancy.oc1..example",
		MaxObjectScan:          3,
		MaxFilesPerRun:         2,
		MaxRecordsPerBatch:     1,
		MaxMemoryBufferRecords: 1,
		MaxRuntime:             "1m",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, client)

	var selected []string
	result, err := collector.CollectStream(context.Background(), time.Time{}, func(_ context.Context, batch providers.FileBatch) error {
		if !batch.Final && len(batch.Records) > 0 {
			selected = append(selected, batch.Metadata.ObjectName)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("CollectStream() error = %v", err)
	}
	if result.FilesSkipped != 1 {
		t.Fatalf("expected newest processed report to be skipped, got %d skipped files", result.FilesSkipped)
	}
	want := []string{
		"reports/cost-csv/0001000000670898.csv.gz",
		"reports/cost-csv/0001000000676802.csv.gz",
	}
	if strings.Join(selected, ",") != strings.Join(want, ",") {
		t.Fatalf("expected next unprocessed reports in OCI list order %v, got %v", want, selected)
	}
	if client.downloads["reports/cost-csv/0001000000999999.csv.gz"] != 0 {
		t.Fatalf("processed newest report should not be downloaded")
	}
}

func TestCollectorRuntimeLimitStopsBeforeNextFile(t *testing.T) {
	repo := &fakeRepository{processed: map[string]bool{}}
	client := &fakeObjectStorageClient{
		namespace: "bling",
		objects: []ObjectInfo{
			{Name: "reports/first.csv", ETag: "etag-1"},
			{Name: "reports/second.csv", ETag: "etag-2"},
		},
		bodies: map[string]string{
			"reports/first.csv": strings.Join([]string{
				"lineItem/intervalUsageStart,product/service,usage/billedQuantity,cost/myCost",
				"2026-05-03T00:00:00Z,Compute,1,3.5",
			}, "\n"),
			"reports/second.csv": strings.Join([]string{
				"lineItem/intervalUsageStart,product/service,usage/billedQuantity,cost/myCost",
				"2026-05-04T00:00:00Z,Compute,1,4.5",
			}, "\n"),
		},
		downloads: map[string]int{},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	collector := NewWithClient(config.Provider{
		Bucket:                 "tenant-bucket",
		Account:                "ocid1.tenancy.oc1..example",
		MaxFilesPerRun:         5,
		MaxRecordsPerBatch:     1,
		MaxMemoryBufferRecords: 1,
		MaxRuntime:             "1ns",
	}, logger, repo, client)

	result, err := collector.CollectStream(context.Background(), time.Time{}, func(context.Context, providers.FileBatch) error {
		return nil
	})
	if err != nil {
		t.Fatalf("CollectStream() error = %v", err)
	}
	if !result.HitRuntimeLimit {
		t.Fatalf("expected runtime limit to be reported")
	}
	if result.FilesProcessed != 1 {
		t.Fatalf("expected first file to finish before runtime stop, got %d", result.FilesProcessed)
	}
	if client.downloads["reports/second.csv"] != 0 {
		t.Fatalf("second file should not be downloaded after runtime limit, got %d", client.downloads["reports/second.csv"])
	}
}

func TestCollectorCleansPartialRecordsAfterParseFailure(t *testing.T) {
	repo := &fakeRepository{processed: map[string]bool{}, deleted: map[string]int{}}
	client := &fakeObjectStorageClient{
		namespace: "bling",
		objects: []ObjectInfo{
			{Name: "reports/bad.csv", ETag: "etag-bad"},
		},
		bodies: map[string]string{
			"reports/bad.csv": strings.Join([]string{
				"lineItem/intervalUsageStart,product/service,usage/billedQuantity,cost/myCost",
				"2026-05-03T00:00:00Z,Compute,1,3.5",
				"\"unterminated",
			}, "\n"),
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	collector := NewWithClient(config.Provider{
		Bucket:                 "tenant-bucket",
		Account:                "ocid1.tenancy.oc1..example",
		MaxFilesPerRun:         5,
		MaxRecordsPerBatch:     1,
		MaxMemoryBufferRecords: 1,
		MaxRuntime:             "1m",
	}, logger, repo, client)

	result, err := collector.CollectStream(context.Background(), time.Time{}, func(context.Context, providers.FileBatch) error {
		return nil
	})
	if err == nil {
		t.Fatalf("expected parse error")
	}
	if result.RecordsProcessed != 1 {
		t.Fatalf("expected one record before parse failure, got %d", result.RecordsProcessed)
	}
	if repo.deleted["reports/bad.csv"] != 1 {
		t.Fatalf("expected partial records cleanup once, got %d", repo.deleted["reports/bad.csv"])
	}
}

type fakeObjectStorageClient struct {
	namespace string
	objects   []ObjectInfo
	bodies    map[string]string
	downloads map[string]int
}

func (f *fakeObjectStorageClient) GetNamespace(context.Context) (string, error) {
	return f.namespace, nil
}

func (f *fakeObjectStorageClient) ListObjects(ctx context.Context, namespace, bucket, prefix string, limit int) ([]ObjectInfo, error) {
	objects, _, err := f.ListObjectsPage(ctx, namespace, bucket, prefix, "", limit)
	return objects, err
}

func (f *fakeObjectStorageClient) ListObjectsPage(_ context.Context, _, _, prefix, start string, limit int) ([]ObjectInfo, string, error) {
	var out []ObjectInfo
	for _, object := range f.objects {
		if prefix != "" && !strings.HasPrefix(object.Name, prefix) {
			continue
		}
		if start != "" && object.Name < start {
			continue
		}
		out = append(out, object)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, "", nil
}

func (f *fakeObjectStorageClient) GetObject(_ context.Context, _, _, objectName string) (io.ReadCloser, error) {
	if f.downloads != nil {
		f.downloads[objectName]++
	}
	return io.NopCloser(strings.NewReader(f.bodies[objectName])), nil
}

type fakeRepository struct {
	processed map[string]bool
	deleted   map[string]int
}

func (f *fakeRepository) AggregateCosts(context.Context, time.Time, time.Time, string) ([]domain.AggregatedCost, error) {
	return nil, nil
}

func (f *fakeRepository) CompareCostVariance(context.Context, string, time.Time, time.Time, time.Time, time.Time) ([]domain.CostVariance, error) {
	return nil, nil
}

func (f *fakeRepository) DetectAnomalies(context.Context, time.Time, time.Time) ([]domain.Anomaly, error) {
	return nil, nil
}

func (f *fakeRepository) ForecastCosts(context.Context, time.Time, time.Time, int) ([]domain.ForecastPoint, error) {
	return nil, nil
}

func (f *fakeRepository) StoreIngestedBatch(context.Context, domain.ProcessedReportFile, []domain.CanonicalCostRecord) error {
	return nil
}

func (f *fakeRepository) StoreCostRecords(context.Context, []domain.CanonicalCostRecord) error {
	return nil
}

func (f *fakeRepository) DeleteCostRecordsForSource(_ context.Context, _ domain.Provider, sourceObject string) error {
	if f.deleted != nil {
		f.deleted[sourceObject]++
	}
	return nil
}

func (f *fakeRepository) MarkReportProcessed(context.Context, domain.ProcessedReportFile) error {
	return nil
}

func (f *fakeRepository) LastIngestionCheckpoint(context.Context, domain.Provider) (time.Time, error) {
	return time.Time{}, nil
}

func (f *fakeRepository) MarkIngestionCheckpoint(context.Context, domain.Provider, time.Time) error {
	return nil
}

func (f *fakeRepository) IsReportProcessed(_ context.Context, _ domain.Provider, _ string, objectName, etag string) (bool, error) {
	return f.processed[objectName+"|"+etag], nil
}

func (f *fakeRepository) IsReportDelivered(context.Context, string, time.Time, time.Time) (bool, error) {
	return false, nil
}

func (f *fakeRepository) RecordReportDelivery(context.Context, string, time.Time, time.Time) error {
	return nil
}

func (f *fakeRepository) ApplyDataLifecyclePolicies(context.Context, int, int) error {
	return nil
}

func (f *fakeRepository) RunDataLifecycleMaintenance(context.Context, int, int) (domain.DataLifecycleMaintenance, error) {
	return domain.DataLifecycleMaintenance{}, nil
}

func (f *fakeRepository) RefreshAggregates(context.Context, time.Time, time.Time) error {
	return nil
}

func (f *fakeRepository) RefreshDashboardAnalytics(context.Context, time.Time, time.Time, int) error {
	return nil
}

func (f *fakeRepository) DashboardOverview(context.Context, time.Time, time.Time, time.Time, time.Time) (domain.DashboardOverview, error) {
	return domain.DashboardOverview{}, nil
}

func (f *fakeRepository) DashboardCostSummaries(context.Context, string, time.Time, time.Time) ([]domain.DashboardCostSummary, error) {
	return []domain.DashboardCostSummary{}, nil
}

func (f *fakeRepository) DashboardAnomalies(context.Context, time.Time, time.Time, string) ([]domain.DashboardAnomaly, error) {
	return []domain.DashboardAnomaly{}, nil
}

func (f *fakeRepository) LatestIngestionStatus(context.Context) (domain.IngestionStatusSummary, error) {
	return domain.IngestionStatusSummary{Providers: []domain.ProviderIngestionStatus{}}, nil
}

func gzipString(t *testing.T, value string) string {
	t.Helper()
	var b strings.Builder
	w := newTestGzipWriter(t, &b)
	if _, err := w.Write([]byte(value)); err != nil {
		t.Fatalf("gzip write failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("gzip close failed: %v", err)
	}
	return b.String()
}

func costReportBodies(t *testing.T, objects []ObjectInfo) map[string]string {
	t.Helper()
	bodies := make(map[string]string, len(objects))
	for _, object := range objects {
		bodies[object.Name] = costReportBody(t, "2026-05-19T00:00:00Z", 3.5)
	}
	return bodies
}

func costReportBody(t *testing.T, usageStart string, cost float64) string {
	t.Helper()
	return gzipString(t, strings.Join([]string{
		"lineItem/intervalUsageStart,lineItem/intervalUsageEnd,product/service,usage/billedQuantity,cost/myCost",
		usageStart + "," + usageStart + ",Compute,1," + strconv.FormatFloat(cost, 'f', 2, 64),
	}, "\n"))
}
