package collect

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/crypticani/cloudpulse/internal/core/normalize"
	"github.com/crypticani/cloudpulse/internal/ports/providers"
	"github.com/crypticani/cloudpulse/internal/ports/storage"
)

type Service struct {
	logger     *slog.Logger
	repo       storage.Repository
	normalizer *normalize.Service
	collectors []providers.Collector
	metrics    MetricsRecorder
}

func New(logger *slog.Logger, repo storage.Repository, normalizer *normalize.Service, collectors []providers.Collector, metrics MetricsRecorder) *Service {
	return &Service{logger: logger, repo: repo, normalizer: normalizer, collectors: collectors, metrics: metrics}
}

func (s *Service) Run(ctx context.Context, since time.Time) error {
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		allErrs []error
	)

	for _, collector := range s.collectors {
		c := collector
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			s.logger.Info("collector start", "provider", c.Name(), "since", since)
			result, err := c.Collect(ctx, since)
			if err != nil {
				mu.Lock()
				allErrs = append(allErrs, err)
				mu.Unlock()
				if s.metrics != nil {
					s.metrics.ObserveFailure(c.Name(), "collect", 1)
				}
				s.logger.Error("collector returned errors", "provider", c.Name(), "error", err)
			}
			for _, batch := range result.Batches {
				normalized := s.normalizer.Normalize(batch.Records)
				batch.Metadata.ProcessedAt = time.Now().UTC()
				batch.Metadata.RecordCount = len(batch.Records)
				batch.Metadata.Status = "processed"
				if err := s.repo.StoreIngestedBatch(ctx, batch.Metadata, normalized); err != nil {
					mu.Lock()
					allErrs = append(allErrs, err)
					mu.Unlock()
					if s.metrics != nil {
						s.metrics.ObserveFailure(c.Name(), "store", 1)
					}
					s.logger.Error("ingested batch store failed", "provider", c.Name(), "object", batch.Metadata.ObjectName, "error", err)
					continue
				}
			}
			if s.metrics != nil {
				status := "success"
				if err != nil {
					status = "partial_failure"
				}
				s.metrics.ObserveCollector(c.Name(), status, time.Since(start))
				s.metrics.ObserveFiles(c.Name(), "processed", result.FilesProcessed)
				s.metrics.ObserveFiles(c.Name(), "skipped", result.FilesSkipped)
				s.metrics.ObserveRecords(c.Name(), result.RecordsProcessed)
				s.metrics.ObserveFailure(c.Name(), "parse", result.Failures)
			}
			s.logger.Info("collector done", "provider", c.Name(), "files_processed", result.FilesProcessed, "files_skipped", result.FilesSkipped, "records", result.RecordsProcessed)
		}()
	}

	wg.Wait()
	return errors.Join(allErrs...)
}
