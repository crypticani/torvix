package oci

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	if limits.MaxRuntime > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, limits.MaxRuntime)
		defer cancel()
	}

	namespace, err := c.resolveNamespace(ctx)
	if err != nil {
		return providers.CollectResult{}, err
	}

	objectScanLimit := c.cfg.MaxObjectScan
	if objectScanLimit <= 0 || objectScanLimit > limits.MaxFilesPerRun {
		objectScanLimit = limits.MaxFilesPerRun
	}
	objects, err := c.client.ListObjects(ctx, namespace, c.cfg.Bucket, c.cfg.Prefix, objectScanLimit)
	if err != nil {
		return providers.CollectResult{}, err
	}
	c.logger.Info("OCI reports discovered", "namespace", namespace, "bucket", c.cfg.Bucket, "prefix", c.cfg.Prefix, "objects", len(objects), "since", since, "max_files_per_run", limits.MaxFilesPerRun, "max_records_per_batch", limits.MaxRecordsPerBatch, "max_runtime", limits.MaxRuntime.String(), "dry_run", limits.DryRun, "sample_mode", limits.SampleMode)
	if len(objects) == 0 {
		c.logger.Warn("no OCI billing reports found", "namespace", namespace, "bucket", c.cfg.Bucket, "prefix", c.cfg.Prefix)
	}

	var (
		result providers.CollectResult
		errs   []error
		seen   = make(map[string]struct{}, len(objects))
	)
	for i, object := range objects {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		if i >= limits.MaxFilesPerRun {
			c.logger.Warn("OCI max files per run reached", "limit", limits.MaxFilesPerRun)
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

		fileStart := time.Now()
		stream, err := c.client.GetObject(ctx, namespace, c.cfg.Bucket, object.Name)
		if err != nil {
			errs = append(errs, err)
			result.Failures++
			c.logger.Error("failed to download OCI report", "object", object.Name, "error", err)
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
		if parseErr != nil {
			errs = append(errs, fmt.Errorf("parse %s: %w", object.Name, parseErr))
			result.Failures++
			c.logger.Error("failed to parse OCI report", "object", object.Name, "error", parseErr)
			continue
		}
		metadata.RecordCount = records
		metadata.ProcessedAt = time.Now().UTC()
		metadata.Status = "processed"
		if err := flush(false); err != nil {
			errs = append(errs, fmt.Errorf("flush %s: %w", object.Name, err))
			result.Failures++
			continue
		}
		if !limits.DryRun {
			if err := handle(ctx, providers.FileBatch{Metadata: metadata, First: firstBatch, Final: true}); err != nil {
				errs = append(errs, fmt.Errorf("mark processed %s: %w", object.Name, err))
				result.Failures++
				continue
			}
		}
		result.FilesProcessed++
		recordsPerSecond := float64(records) / max(time.Since(fileStart).Seconds(), 0.001)
		c.logger.Info("OCI report streamed", "object", object.Name, "records", records, "duration", time.Since(fileStart).String(), "records_per_second", recordsPerSecond)
	}

	return result, errors.Join(errs...)
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
