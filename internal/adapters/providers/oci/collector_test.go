package oci

import (
	"context"
	"io"
	"log/slog"
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

func (f *fakeObjectStorageClient) ListObjects(context.Context, string, string, string, int) ([]ObjectInfo, error) {
	return f.objects, nil
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

func (f *fakeRepository) ApplyDataLifecyclePolicies(context.Context, int, int) error {
	return nil
}

func (f *fakeRepository) RunDataLifecycleMaintenance(context.Context, int, int) (domain.DataLifecycleMaintenance, error) {
	return domain.DataLifecycleMaintenance{}, nil
}

func (f *fakeRepository) RefreshAggregates(context.Context, time.Time, time.Time) error {
	return nil
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
