package oci

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/crypticani/cloudpulse/internal/config"
	"github.com/crypticani/cloudpulse/internal/domain"
	"github.com/crypticani/cloudpulse/internal/ports/providers"
	"github.com/crypticani/cloudpulse/internal/ports/storage"
)

type Collector struct {
	cfg       config.Provider
	logger    *slog.Logger
	repo      storage.Repository
	client    ObjectStorageClient
	parser    *Parser
	namespace string
}

const (
	ociCostReportPrefix                               = "reports/cost-csv/"
	ociObjectListOrderSelectionStrategy               = "oci_object_list_order"
	ociCostReportTimeModifiedSelectionStrategy        = "oci_cost_report_time_modified_desc"
	maxCostReportStartSearchProbes                    = 32
	initialCostReportStartSearchNumericStep    uint64 = 100000
)

func New(cfg config.Provider, logger *slog.Logger, repo storage.Repository) (*Collector, error) {
	client, err := NewObjectStorageClient(cfg)
	if err != nil {
		return nil, err
	}
	return NewWithClient(cfg, logger, repo, client), nil
}

func NewWithClient(cfg config.Provider, logger *slog.Logger, repo storage.Repository, client ObjectStorageClient) *Collector {
	return &Collector{
		cfg:       cfg,
		logger:    logger,
		repo:      repo,
		client:    client,
		parser:    NewParser(),
		namespace: strings.TrimSpace(cfg.Namespace),
	}
}

func (c *Collector) Name() string { return "oci" }

func (c *Collector) Collect(ctx context.Context, since time.Time) (providers.CollectResult, error) {
	if _, ok := any(c).(providers.StreamCollector); ok {
		return c.CollectStream(ctx, since, func(context.Context, providers.FileBatch) error {
			return nil
		})
	}
	return providers.CollectResult{}, fmt.Errorf("oci collector requires streaming ingestion")
}

func (c *Collector) CollectStream(ctx context.Context, since time.Time, handle providers.BatchHandler) (providers.CollectResult, error) {
	limits := c.cfg.IngestionLimits()
	var runDeadline time.Time
	if limits.MaxRuntime > 0 {
		runDeadline = time.Now().Add(limits.MaxRuntime)
	}

	namespace, err := c.resolveNamespace(ctx)
	if err != nil {
		return providers.CollectResult{}, err
	}

	objectScanLimit := c.objectScanLimit(limits.MaxFilesPerRun)
	objects, selectionStrategy, err := c.listCandidateObjects(ctx, namespace, since, objectScanLimit)
	if err != nil {
		return providers.CollectResult{}, err
	}
	c.logger.Info("OCI reports discovered", "namespace", namespace, "bucket", c.cfg.Bucket, "prefix", c.cfg.Prefix, "objects", len(objects), "candidate_objects", len(objects), "selection_strategy", selectionStrategy, "object_scan_limit", objectScanLimit, "since", since, "max_files_per_run", limits.MaxFilesPerRun, "max_records_per_batch", limits.MaxRecordsPerBatch, "max_runtime", limits.MaxRuntime.String(), "dry_run", limits.DryRun, "sample_mode", limits.SampleMode)
	if len(objects) == 0 {
		c.logger.Warn("no OCI billing reports found", "namespace", namespace, "bucket", c.cfg.Bucket, "prefix", c.cfg.Prefix)
	}

	var (
		result                    providers.CollectResult
		errs                      []error
		seen                      = make(map[string]struct{}, len(objects))
		startedFiles              int
		consecutiveZeroYieldFiles int
		consecutiveFailures       int
		selectedObjects           []string
	)
	const maxConsecutiveObjectFailures = 3
	for _, object := range objects {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		if !runDeadline.IsZero() && startedFiles > 0 && time.Now().After(runDeadline) {
			result.HitRuntimeLimit = true
			c.logger.Warn("OCI max runtime reached before starting next report", "limit", limits.MaxRuntime, "files_processed", result.FilesProcessed, "records_processed", result.RecordsProcessed)
			break
		}
		identity := object.Name + "|" + object.ETag
		if _, ok := seen[identity]; ok {
			result.FilesSkipped++
			c.logger.Warn("skipping repeated OCI object in same run", "object", object.Name, "etag", object.ETag)
			continue
		}
		seen[identity] = struct{}{}

		processed, err := c.repo.IsReportProcessed(ctx, domain.ProviderOCI, c.cfg.Bucket, object.Name, object.ETag)
		if err != nil {
			errs = append(errs, fmt.Errorf("check processed %s: %w", object.Name, err))
			result.Failures++
			continue
		}
		if processed {
			result.FilesSkipped++
			c.logger.Debug("skipping processed OCI report", "object", object.Name, "etag", object.ETag)
			continue
		}
		if startedFiles >= limits.MaxFilesPerRun {
			result.HitFileLimit = true
			c.logger.Warn("OCI max files per run reached", "limit", limits.MaxFilesPerRun)
			break
		}
		startedFiles++
		selectedObjects = append(selectedObjects, object.Name)

		fileStart := time.Now()
		stream, err := c.client.GetObject(ctx, namespace, c.cfg.Bucket, object.Name)
		if err != nil {
			errs = append(errs, err)
			result.Failures++
			consecutiveFailures++
			c.logger.Error("failed to download OCI report", "object", object.Name, "error", err)
			if consecutiveFailures >= maxConsecutiveObjectFailures {
				c.logger.Warn("stopping OCI ingestion after consecutive object failures", "failures", consecutiveFailures)
				break
			}
			continue
		}
		c.logger.Info("OCI report streaming started", "object", object.Name, "etag", object.ETag, "size", object.Size)

		metadata := domain.ProcessedReportFile{
			Provider:     domain.ProviderOCI,
			Bucket:       c.cfg.Bucket,
			ObjectName:   object.Name,
			ETag:         object.ETag,
			LastModified: object.LastModified,
		}
		buffer := make([]domain.RawBillingRecord, 0, limits.MaxMemoryBufferRecords)
		firstBatch := true
		flush := func(final bool) error {
			if len(buffer) == 0 && !final {
				return nil
			}
			batchRecords := buffer
			if limits.DryRun {
				c.logger.Info("OCI dry-run batch parsed", "object", object.Name, "records", len(batchRecords), "final", final)
				buffer = make([]domain.RawBillingRecord, 0, limits.MaxMemoryBufferRecords)
				return nil
			}
			if err := handle(ctx, providers.FileBatch{
				Metadata: metadata,
				Records:  batchRecords,
				First:    firstBatch,
				Final:    final,
			}); err != nil {
				return err
			}
			firstBatch = false
			if len(batchRecords) > 0 {
				result.BatchesInserted++
				c.logger.Info("OCI report batch flushed", "object", object.Name, "records", len(batchRecords), "batches_inserted", result.BatchesInserted, "records_processed", result.RecordsProcessed)
			}
			buffer = make([]domain.RawBillingRecord, 0, limits.MaxMemoryBufferRecords)
			return nil
		}

		recordsWithinSelectionWindow := 0
		timestamps := newTimestampSummary()
		records, parseErr := c.parser.ParseStream(ctx, stream, object.Name, c.cfg.Account, func(record domain.RawBillingRecord) error {
			timestamps.Observe(record.UsageStart)
			if recordInSelectionWindow(record, since) {
				recordsWithinSelectionWindow++
			}
			buffer = append(buffer, record)
			result.RecordsProcessed++
			if len(buffer) >= limits.MaxMemoryBufferRecords {
				return flush(false)
			}
			return nil
		})
		if closeErr := stream.Close(); closeErr != nil {
			c.logger.Warn("failed to close OCI report stream", "object", object.Name, "error", closeErr)
		}
		if parseErr != nil {
			errs = append(errs, fmt.Errorf("parse %s: %w", object.Name, parseErr))
			result.Failures++
			consecutiveFailures++
			c.logger.Error("failed to parse OCI report", "object", object.Name, "error", parseErr)
			if cleanupErr := c.cleanupPartialIngest(object.Name); cleanupErr != nil {
				errs = append(errs, fmt.Errorf("cleanup partial %s: %w", object.Name, cleanupErr))
			}
			if consecutiveFailures >= maxConsecutiveObjectFailures || ctx.Err() != nil {
				c.logger.Warn("stopping OCI ingestion after parse failure", "failures", consecutiveFailures, "context_error", ctx.Err())
				break
			}
			continue
		}
		metadata.RecordCount = records
		metadata.ProcessedAt = time.Now().UTC()
		metadata.Status = "processed"
		if err := flush(false); err != nil {
			errs = append(errs, fmt.Errorf("flush %s: %w", object.Name, err))
			result.Failures++
			consecutiveFailures++
			if cleanupErr := c.cleanupPartialIngest(object.Name); cleanupErr != nil {
				errs = append(errs, fmt.Errorf("cleanup partial %s: %w", object.Name, cleanupErr))
			}
			if consecutiveFailures >= maxConsecutiveObjectFailures || ctx.Err() != nil {
				c.logger.Warn("stopping OCI ingestion after flush failure", "failures", consecutiveFailures, "context_error", ctx.Err())
				break
			}
			continue
		}
		if !limits.DryRun {
			if err := handle(ctx, providers.FileBatch{Metadata: metadata, First: firstBatch, Final: true}); err != nil {
				errs = append(errs, fmt.Errorf("mark processed %s: %w", object.Name, err))
				result.Failures++
				consecutiveFailures++
				if cleanupErr := c.cleanupPartialIngest(object.Name); cleanupErr != nil {
					errs = append(errs, fmt.Errorf("cleanup partial %s: %w", object.Name, cleanupErr))
				}
				if consecutiveFailures >= maxConsecutiveObjectFailures || ctx.Err() != nil {
					c.logger.Warn("stopping OCI ingestion after mark-processed failure", "failures", consecutiveFailures, "context_error", ctx.Err())
					break
				}
				continue
			}
		}
		result.FilesProcessed++
		if recordsWithinSelectionWindow > 0 {
			consecutiveZeroYieldFiles = 0
		} else {
			consecutiveZeroYieldFiles++
		}
		consecutiveFailures = 0
		recordsPerSecond := float64(records) / max(time.Since(fileStart).Seconds(), 0.001)
		c.logger.Info("OCI report streamed", "object", object.Name, "records", records, "records_within_selection_window", recordsWithinSelectionWindow, "selection_window_start", since, "timestamp_min", timestamps.Min, "timestamp_max", timestamps.Max, "timestamp_month_counts", timestamps.MonthCounts(), "consecutive_zero_yield_files", consecutiveZeroYieldFiles, "duration", time.Since(fileStart).String(), "records_per_second", recordsPerSecond)
		if consecutiveZeroYieldFiles >= limits.MaxZeroYieldFiles {
			result.HitZeroYieldLimit = true
			c.logger.Warn("stopping OCI scan after consecutive zero-yield files", "limit", limits.MaxZeroYieldFiles, "files_processed", result.FilesProcessed, "records_processed", result.RecordsProcessed, "last_object", object.Name)
			break
		}
	}
	c.logger.Info("OCI report object selection completed", "objects_discovered", len(objects), "candidate_objects", len(objects), "selected_objects", len(selectedObjects), "selected_object_names", selectedObjects, "first_selected_object", firstString(selectedObjects), "last_selected_object", lastString(selectedObjects), "consecutive_zero_yield_files", consecutiveZeroYieldFiles, "hit_zero_yield_limit", result.HitZeroYieldLimit, "selection_strategy", selectionStrategy)

	return result, errors.Join(errs...)
}

func (c *Collector) objectScanLimit(maxFilesPerRun int) int {
	if c.cfg.MaxObjectScan > 0 {
		return c.cfg.MaxObjectScan
	}
	return maxFilesPerRun
}

func (c *Collector) cleanupPartialIngest(objectName string) error {
	if c.repo == nil || objectName == "" {
		return nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := c.repo.DeleteCostRecordsForSource(cleanupCtx, domain.ProviderOCI, objectName); err != nil {
		c.logger.Error("failed to clean up partial OCI report records", "object", objectName, "error", err)
		return err
	}
	c.logger.Info("partial OCI report records cleaned up", "object", objectName)
	return nil
}

func (c *Collector) resolveNamespace(ctx context.Context) (string, error) {
	if c.namespace != "" {
		return c.namespace, nil
	}
	namespace, err := c.client.GetNamespace(ctx)
	if err != nil {
		return "", err
	}
	c.namespace = namespace
	return namespace, nil
}

func (c *Collector) listCandidateObjects(ctx context.Context, namespace string, since time.Time, limit int) ([]ObjectInfo, string, error) {
	if c.shouldPreferCostReportObjects() && !since.IsZero() {
		objects, err := c.listRecentCostReportObjects(ctx, namespace, since, limit)
		if err != nil {
			return nil, "", err
		}
		if len(objects) > 0 {
			return objects, ociCostReportTimeModifiedSelectionStrategy, nil
		}
		c.logger.Warn("recent OCI cost report selection returned no candidates; falling back to configured prefix listing", "prefix", c.cfg.Prefix, "cost_report_prefix", ociCostReportPrefix, "since", since)
	}
	objects, err := c.client.ListObjects(ctx, namespace, c.cfg.Bucket, c.cfg.Prefix, limit)
	if err != nil {
		return nil, "", err
	}
	ranked, strategy := rankReportObjects(objects)
	return ranked, strategy, nil
}

func (c *Collector) shouldPreferCostReportObjects() bool {
	prefix := strings.TrimSpace(c.cfg.Prefix)
	return prefix == "" || prefix == "reports/" || strings.HasPrefix(prefix, ociCostReportPrefix)
}

func (c *Collector) listRecentCostReportObjects(ctx context.Context, namespace string, since time.Time, limit int) ([]ObjectInfo, error) {
	start := ""
	startNumber, ok, err := c.findCostReportStartNumber(ctx, namespace, since)
	if err != nil {
		return nil, err
	}
	if ok {
		start = costReportStartName(startNumber)
	}
	objects, err := c.listObjectsFrom(ctx, namespace, ociCostReportPrefix, start, limit)
	if err != nil {
		return nil, err
	}
	cutoff := since.Add(-24 * time.Hour)
	filtered := objects[:0]
	for _, object := range objects {
		if !strings.HasPrefix(object.Name, ociCostReportPrefix) {
			continue
		}
		if !object.LastModified.IsZero() && object.LastModified.Before(cutoff) {
			continue
		}
		filtered = append(filtered, object)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if !filtered[i].LastModified.Equal(filtered[j].LastModified) {
			return filtered[i].LastModified.After(filtered[j].LastModified)
		}
		return filtered[i].Name > filtered[j].Name
	})
	return filtered, nil
}

func (c *Collector) listObjectsFrom(ctx context.Context, namespace, prefix, start string, limit int) ([]ObjectInfo, error) {
	var out []ObjectInfo
	seenPageStart := map[string]struct{}{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pageLimit := 1000
		if limit > 0 && limit-len(out) < pageLimit {
			pageLimit = limit - len(out)
		}
		if pageLimit <= 0 {
			return out, nil
		}
		objects, nextStart, err := c.client.ListObjectsPage(ctx, namespace, c.cfg.Bucket, prefix, start, pageLimit)
		if err != nil {
			return nil, err
		}
		if len(objects) == 0 {
			return out, nil
		}
		out = append(out, objects...)
		if limit > 0 && len(out) >= limit {
			return out, nil
		}
		if nextStart == "" {
			return out, nil
		}
		if _, ok := seenPageStart[nextStart]; ok {
			return nil, fmt.Errorf("list objects pagination repeated token %q", nextStart)
		}
		seenPageStart[nextStart] = struct{}{}
		start = nextStart
	}
}

func (c *Collector) findCostReportStartNumber(ctx context.Context, namespace string, since time.Time) (uint64, bool, error) {
	first, ok, err := c.firstCostReportAtOrAfter(ctx, namespace, "")
	if err != nil || !ok {
		return 0, false, err
	}
	firstNumber, ok := costReportNumber(first.Name)
	if !ok || first.LastModified.IsZero() {
		return 0, false, nil
	}
	if !first.LastModified.Before(since) {
		return firstNumber, true, nil
	}

	low := firstNumber
	high := firstNumber + initialCostReportStartSearchNumericStep
	for probe := 0; probe < maxCostReportStartSearchProbes; probe++ {
		object, found, err := c.firstCostReportAtOrAfter(ctx, namespace, costReportStartName(high))
		if err != nil {
			return 0, false, err
		}
		if !found {
			break
		}
		objectNumber, ok := costReportNumber(object.Name)
		if !ok || object.LastModified.IsZero() {
			return 0, false, nil
		}
		if !object.LastModified.Before(since) {
			high = objectNumber
			return c.binarySearchCostReportStart(ctx, namespace, since, low, high)
		}
		if objectNumber <= low {
			low = high
		} else {
			low = objectNumber
		}
		step := initialCostReportStartSearchNumericStep << min(probe+1, 20)
		high = low + step
	}
	return 0, false, nil
}

func (c *Collector) binarySearchCostReportStart(ctx context.Context, namespace string, since time.Time, low, high uint64) (uint64, bool, error) {
	best := high
	for probe := 0; probe < maxCostReportStartSearchProbes && low+1 < high; probe++ {
		mid := low + (high-low)/2
		object, found, err := c.firstCostReportAtOrAfter(ctx, namespace, costReportStartName(mid))
		if err != nil {
			return 0, false, err
		}
		if !found {
			high = mid
			continue
		}
		objectNumber, ok := costReportNumber(object.Name)
		if !ok || object.LastModified.IsZero() {
			return 0, false, nil
		}
		if object.LastModified.Before(since) {
			if objectNumber <= low {
				low = mid
			} else {
				low = objectNumber
			}
			continue
		}
		best = objectNumber
		high = objectNumber
	}
	return best, true, nil
}

func (c *Collector) firstCostReportAtOrAfter(ctx context.Context, namespace, start string) (ObjectInfo, bool, error) {
	objects, _, err := c.client.ListObjectsPage(ctx, namespace, c.cfg.Bucket, ociCostReportPrefix, start, 1)
	if err != nil {
		return ObjectInfo{}, false, err
	}
	if len(objects) == 0 {
		return ObjectInfo{}, false, nil
	}
	if !strings.HasPrefix(objects[0].Name, ociCostReportPrefix) {
		return ObjectInfo{}, false, nil
	}
	return objects[0], true, nil
}

func rankReportObjects(objects []ObjectInfo) ([]ObjectInfo, string) {
	ranked := append([]ObjectInfo(nil), objects...)
	return ranked, ociObjectListOrderSelectionStrategy
}

func costReportStartName(number uint64) string {
	return fmt.Sprintf("%s%016d", ociCostReportPrefix, number)
}

func costReportNumber(objectName string) (uint64, bool) {
	if !strings.HasPrefix(objectName, ociCostReportPrefix) {
		return 0, false
	}
	base := strings.TrimPrefix(objectName, ociCostReportPrefix)
	if idx := strings.Index(base, "-"); idx >= 0 {
		base = base[:idx]
	}
	base = strings.TrimSuffix(base, ".gz")
	base = strings.TrimSuffix(base, ".csv")
	if base == "" {
		return 0, false
	}
	for _, r := range base {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	number, err := strconv.ParseUint(base, 10, 64)
	return number, err == nil
}

func recordInSelectionWindow(record domain.RawBillingRecord, since time.Time) bool {
	if since.IsZero() || record.UsageStart.IsZero() {
		return true
	}
	return !record.UsageStart.Before(since)
}

type timestampSummary struct {
	Count  int
	Min    time.Time
	Max    time.Time
	Months map[string]int
}

func newTimestampSummary() *timestampSummary {
	return &timestampSummary{Months: map[string]int{}}
}

func (s *timestampSummary) Observe(value time.Time) {
	if value.IsZero() {
		return
	}
	value = value.UTC()
	if s.Count == 0 || value.Before(s.Min) {
		s.Min = value
	}
	if s.Count == 0 || value.After(s.Max) {
		s.Max = value
	}
	s.Count++
	s.Months[value.Format("2006-01")]++
}

func (s *timestampSummary) MonthCounts() []string {
	if len(s.Months) == 0 {
		return nil
	}
	keys := make([]string, 0, len(s.Months))
	for key := range s.Months {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, fmt.Sprintf("%s=%d", key, s.Months[key]))
	}
	return out
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func lastString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}
