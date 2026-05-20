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
	ociCostReportPrefix                   = "reports/cost-csv/"
	ociCostReportNumericSelectionStrategy = "oci_cost_report_numeric_desc"
	ociObjectListOrderSelectionStrategy   = "oci_object_list_order"
	defaultOCICostReportObjectScanLimit   = 10000
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
	objects, err := c.client.ListObjects(ctx, namespace, c.cfg.Bucket, c.cfg.Prefix, objectScanLimit)
	if err != nil {
		return providers.CollectResult{}, err
	}
	rankedObjects, selectionStrategy := rankReportObjects(objects)
	c.logger.Info("OCI reports discovered", "namespace", namespace, "bucket", c.cfg.Bucket, "prefix", c.cfg.Prefix, "objects", len(objects), "candidate_objects", len(rankedObjects), "selection_strategy", selectionStrategy, "object_scan_limit", objectScanLimit, "since", since, "max_files_per_run", limits.MaxFilesPerRun, "max_records_per_batch", limits.MaxRecordsPerBatch, "max_runtime", limits.MaxRuntime.String(), "dry_run", limits.DryRun, "sample_mode", limits.SampleMode)
	if len(objects) == 0 {
		c.logger.Warn("no OCI billing reports found", "namespace", namespace, "bucket", c.cfg.Bucket, "prefix", c.cfg.Prefix)
	}

	var (
		result              providers.CollectResult
		errs                []error
		seen                = make(map[string]struct{}, len(objects))
		startedFiles        int
		consecutiveFailures int
		selectedObjects     []string
	)
	const maxConsecutiveObjectFailures = 3
	for _, object := range rankedObjects {
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

		records, parseErr := c.parser.ParseStream(ctx, stream, object.Name, c.cfg.Account, func(record domain.RawBillingRecord) error {
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
		consecutiveFailures = 0
		recordsPerSecond := float64(records) / max(time.Since(fileStart).Seconds(), 0.001)
		c.logger.Info("OCI report streamed", "object", object.Name, "records", records, "duration", time.Since(fileStart).String(), "records_per_second", recordsPerSecond)
	}
	c.logger.Info("OCI report object selection completed", "objects_discovered", len(objects), "candidate_objects", len(rankedObjects), "selected_objects", len(selectedObjects), "selected_object_names", selectedObjects, "first_selected_object", firstString(selectedObjects), "last_selected_object", lastString(selectedObjects), "selection_strategy", selectionStrategy)

	return result, errors.Join(errs...)
}

func (c *Collector) objectScanLimit(maxFilesPerRun int) int {
	if c.cfg.MaxObjectScan > 0 {
		return c.cfg.MaxObjectScan
	}
	if isOCICostReportPrefix(c.cfg.Prefix) {
		return defaultOCICostReportObjectScanLimit
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

func rankReportObjects(objects []ObjectInfo) ([]ObjectInfo, string) {
	ranked := append([]ObjectInfo(nil), objects...)
	hasNumericCostReports := false
	for _, object := range ranked {
		if _, ok := costReportNumber(object.Name); ok {
			hasNumericCostReports = true
			break
		}
	}
	if !hasNumericCostReports {
		return ranked, ociObjectListOrderSelectionStrategy
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		leftNumber, leftOK := costReportNumber(ranked[i].Name)
		rightNumber, rightOK := costReportNumber(ranked[j].Name)
		if leftOK != rightOK {
			return leftOK
		}
		if leftOK && rightOK && leftNumber != rightNumber {
			return leftNumber > rightNumber
		}
		if !ranked[i].LastModified.Equal(ranked[j].LastModified) {
			return ranked[i].LastModified.After(ranked[j].LastModified)
		}
		return ranked[i].Name > ranked[j].Name
	})
	return ranked, ociCostReportNumericSelectionStrategy
}

func costReportNumber(objectName string) (uint64, bool) {
	if !isOCICostReportPrefix(objectName) {
		return 0, false
	}
	base := objectName
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
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
	if err != nil {
		return 0, false
	}
	return number, true
}

func isOCICostReportPrefix(value string) bool {
	return strings.HasPrefix(value, ociCostReportPrefix)
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
