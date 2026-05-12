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
	namespace, err := c.resolveNamespace(ctx)
	if err != nil {
		return providers.CollectResult{}, err
	}

	objects, err := c.client.ListObjects(ctx, namespace, c.cfg.Bucket, c.cfg.Prefix, c.cfg.MaxObjectScan)
	if err != nil {
		return providers.CollectResult{}, err
	}

	var (
		result providers.CollectResult
		errs   []error
	)
	for _, object := range objects {
		if !object.LastModified.IsZero() && object.LastModified.Before(since) {
			continue
		}
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

		stream, err := c.client.GetObject(ctx, namespace, c.cfg.Bucket, object.Name)
		if err != nil {
			errs = append(errs, err)
			result.Failures++
			c.logger.Error("failed to download OCI report", "object", object.Name, "error", err)
			continue
		}
		records, parseErr := c.parser.Parse(stream, object.Name, c.cfg.Account)
		if parseErr != nil {
			errs = append(errs, fmt.Errorf("parse %s: %w", object.Name, parseErr))
			result.Failures++
			c.logger.Error("failed to parse OCI report", "object", object.Name, "error", parseErr)
			continue
		}

		result.Batches = append(result.Batches, providers.FileBatch{
			Metadata: domain.ProcessedReportFile{
				Provider:     domain.ProviderOCI,
				Bucket:       c.cfg.Bucket,
				ObjectName:   object.Name,
				ETag:         object.ETag,
				LastModified: object.LastModified,
			},
			Records: records,
		})
		result.FilesProcessed++
		result.RecordsProcessed += len(records)
		c.logger.Info("OCI report parsed", "object", object.Name, "records", len(records))
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
