package collect

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/crypticani/cloudpulse/internal/core/normalize"
	"github.com/crypticani/cloudpulse/internal/domain"
	"github.com/crypticani/cloudpulse/internal/ports/providers"
	"github.com/crypticani/cloudpulse/internal/ports/storage"
)

type Service struct {
	logger     *slog.Logger
	repo       storage.Repository
	normalizer *normalize.Service
	collectors []providers.Collector
	metrics    MetricsRecorder
	policy     Policy
}

type Policy struct {
	LookbackDays         int
	RetentionDays        int
	CompressionAfterDays int
}

func New(logger *slog.Logger, repo storage.Repository, normalizer *normalize.Service, collectors []providers.Collector, metrics MetricsRecorder) *Service {
	return NewWithPolicy(logger, repo, normalizer, collectors, metrics, Policy{})
}

func NewWithPolicy(logger *slog.Logger, repo storage.Repository, normalizer *normalize.Service, collectors []providers.Collector, metrics MetricsRecorder, policy Policy) *Service {
	if policy.LookbackDays <= 0 {
		policy.LookbackDays = 30
	}
	if policy.RetentionDays <= 0 {
		policy.RetentionDays = 180
	}
	if policy.CompressionAfterDays <= 0 {
		policy.CompressionAfterDays = 7
	}
	return &Service{logger: logger, repo: repo, normalizer: normalizer, collectors: collectors, metrics: metrics, policy: policy}
}

// ProviderResult holds per-provider ingestion metrics returned by Run.
type ProviderResult struct {
	Provider        string        `json:"provider"`
	FilesProcessed  int           `json:"files_processed"`
	FilesSkipped    int           `json:"files_skipped"`
	SkippedOldFiles int           `json:"skipped_old_files"`
	RecordsParsed   int           `json:"records_parsed"`
	RecordsInserted int           `json:"records_inserted"`
	Duration        time.Duration `json:"duration_ns"`
	EarliestRecord  time.Time     `json:"earliest_record"`
	Error           string        `json:"error,omitempty"`
}

func (s *Service) Run(ctx context.Context, since time.Time) ([]ProviderResult, error) {
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		allErrs []error
		results []ProviderResult
	)

	for _, collector := range s.collectors {
		c := collector
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			if s.logger != nil {
				s.logger.Info("collector start", "provider", c.Name(), "requested_since", since)
			}

			pr := ProviderResult{Provider: c.Name()}
			recordsInserted := 0
			effectiveSince := s.effectiveSince(ctx, c.Name(), since)
			if s.logger != nil {
				s.logger.Info("collector rolling window selected", "provider", c.Name(), "since", effectiveSince, "lookback_days", s.policy.LookbackDays)
			}
			result, err := s.collectProvider(ctx, c, effectiveSince, &pr, &recordsInserted)
			if err != nil {
				mu.Lock()
				allErrs = append(allErrs, err)
				mu.Unlock()
				if s.metrics != nil {
					s.metrics.ObserveFailure(c.Name(), "collect", 1)
				}
				if s.logger != nil {
					s.logger.Error("collector returned errors", "provider", c.Name(), "error", err)
				}
				pr.Error = err.Error()
			}

			pr.FilesProcessed = result.FilesProcessed
			pr.FilesSkipped = result.FilesSkipped
			pr.SkippedOldFiles = result.SkippedOldFiles
			pr.RecordsParsed = result.RecordsProcessed
			pr.RecordsInserted = recordsInserted
			pr.Duration = time.Since(start)

			if s.metrics != nil {
				status := "success"
				if err != nil {
					status = "partial_failure"
				}
				s.metrics.ObserveCollector(c.Name(), status, pr.Duration)
				s.metrics.ObserveFiles(c.Name(), "processed", result.FilesProcessed)
				s.metrics.ObserveFiles(c.Name(), "skipped", result.FilesSkipped)
				s.metrics.ObserveFiles(c.Name(), "skipped_old", result.SkippedOldFiles)
				s.metrics.ObserveRecords(c.Name(), result.RecordsProcessed)
				s.metrics.ObserveBatches(c.Name(), result.BatchesInserted)
				s.metrics.ObserveRecordsPerSecond(c.Name(), float64(result.RecordsProcessed)/max(pr.Duration.Seconds(), 0.001))
				s.metrics.ObserveFailure(c.Name(), "parse", result.Failures)
			}
			if s.logger != nil {
				s.logger.Info("collector done", "provider", c.Name(), "files_processed", result.FilesProcessed, "files_skipped", result.FilesSkipped, "skipped_old_files", result.SkippedOldFiles, "records_parsed", result.RecordsProcessed, "records_inserted", recordsInserted, "duration", pr.Duration.String())
			}
			if err == nil && result.FilesProcessed > 0 && !result.HitFileLimit && !result.HitRuntimeLimit {
				if checkpointErr := s.repo.MarkIngestionCheckpoint(ctx, providerName(c.Name()), time.Now().UTC()); checkpointErr != nil {
					mu.Lock()
					allErrs = append(allErrs, checkpointErr)
					mu.Unlock()
					if s.logger != nil {
						s.logger.Error("failed to update ingestion checkpoint", "provider", c.Name(), "error", checkpointErr)
					}
				}
			} else if err == nil && s.logger != nil {
				s.logger.Info("ingestion checkpoint unchanged", "provider", c.Name(), "files_processed", result.FilesProcessed, "hit_file_limit", result.HitFileLimit, "hit_runtime_limit", result.HitRuntimeLimit)
			}

			mu.Lock()
			results = append(results, pr)
			mu.Unlock()
		}()
	}

	wg.Wait()

	// Refresh continuous aggregates so analytics queries see the new data immediately.
	totalInserted := 0
	earliestRecordTime := time.Now().UTC()
	for _, pr := range results {
		totalInserted += pr.RecordsInserted
		if !pr.EarliestRecord.IsZero() && pr.EarliestRecord.Before(earliestRecordTime) {
			earliestRecordTime = pr.EarliestRecord
		}
	}
	if totalInserted > 0 {
		refreshSince := earliestRecordTime.Truncate(24 * time.Hour)
		// Fallback to the requested 'since' if earliestRecordTime is somehow zero.
		if refreshSince.IsZero() || refreshSince.After(time.Now().UTC()) {
			refreshSince = since
		}
		refreshEnd := time.Now().UTC().Add(24 * time.Hour) // Buffer for bucket boundaries.
		if err := s.repo.RefreshAggregates(ctx, refreshSince, refreshEnd); err != nil {
			if s.logger != nil {
				s.logger.Error("failed to refresh aggregates", "error", err)
			}
		} else {
			if s.logger != nil {
				s.logger.Info("continuous aggregates refreshed", "from", refreshSince, "to", refreshEnd)
			}
		}
	}

	maintenance, err := s.repo.RunDataLifecycleMaintenance(ctx, s.policy.RetentionDays, s.policy.CompressionAfterDays)
	if err != nil {
		allErrs = append(allErrs, err)
		if s.logger != nil {
			s.logger.Error("data lifecycle maintenance failed", "error", err)
		}
	} else {
		if s.metrics != nil {
			s.metrics.ObserveRecordsDeleted(maintenance.RecordsDeleted)
			s.metrics.ObserveCompressedChunks(maintenance.CompressedChunks)
		}
		if s.logger != nil {
			s.logger.Info("rolling retention maintenance complete", "records_deleted", maintenance.RecordsDeleted, "compressed_chunks", maintenance.CompressedChunks)
		}
	}

	return results, errors.Join(allErrs...)
}

func (s *Service) effectiveSince(ctx context.Context, provider string, requested time.Time) time.Time {
	now := time.Now().UTC()
	lookbackSince := now.AddDate(0, 0, -s.policy.LookbackDays)
	effective := requested.UTC()
	if effective.IsZero() || effective.Before(lookbackSince) {
		effective = lookbackSince
	}
	checkpoint, err := s.repo.LastIngestionCheckpoint(ctx, providerName(provider))
	if err != nil {
		if s.logger != nil {
			s.logger.Error("failed to read ingestion checkpoint", "provider", provider, "error", err)
		}
		return effective
	}
	if checkpoint.After(effective) {
		overlap := checkpoint.Add(-1 * time.Hour)
		if overlap.After(effective) {
			effective = overlap
		}
	}
	return effective
}

func providerName(name string) domain.Provider {
	return domain.Provider(name)
}

func (s *Service) collectProvider(ctx context.Context, c providers.Collector, since time.Time, pr *ProviderResult, recordsInserted *int) (providers.CollectResult, error) {
	if sc, ok := c.(providers.StreamCollector); ok {
		return sc.CollectStream(ctx, since, func(ctx context.Context, batch providers.FileBatch) error {
			if batch.Final {
				if batch.First {
					if err := s.repo.DeleteCostRecordsForSource(ctx, batch.Metadata.Provider, batch.Metadata.ObjectName); err != nil {
						return err
					}
				}
				batch.Metadata.ProcessedAt = time.Now().UTC()
				batch.Metadata.Status = "processed"
				if err := s.repo.MarkReportProcessed(ctx, batch.Metadata); err != nil {
					if s.metrics != nil {
						s.metrics.ObserveFailure(c.Name(), "mark_processed", 1)
					}
					return err
				}
				if s.logger != nil {
					s.logger.Info("report marked processed", "provider", c.Name(), "object", batch.Metadata.ObjectName, "records", batch.Metadata.RecordCount)
				}
				return nil
			}
			return s.storeNormalizedBatch(ctx, c.Name(), batch, pr, recordsInserted, true)
		})
	}

	result, err := c.Collect(ctx, since)
	for _, batch := range result.Batches {
		if storeErr := s.storeNormalizedBatch(ctx, c.Name(), batch, pr, recordsInserted, false); storeErr != nil {
			result.Failures++
			if err == nil {
				err = storeErr
			} else {
				err = errors.Join(err, storeErr)
			}
		}
	}
	return result, err
}

func (s *Service) storeNormalizedBatch(ctx context.Context, provider string, batch providers.FileBatch, pr *ProviderResult, recordsInserted *int, streaming bool) error {
	normalized := s.normalizer.Normalize(batch.Records)
	if s.logger != nil {
		s.logger.Info("records normalized", "provider", provider, "object", batch.Metadata.ObjectName, "records_parsed", len(batch.Records), "records_normalized", len(normalized), "streaming", streaming)
	}
	var err error
	if streaming {
		if batch.First {
			if err := s.repo.DeleteCostRecordsForSource(ctx, batch.Metadata.Provider, batch.Metadata.ObjectName); err != nil {
				return err
			}
		}
		err = s.repo.StoreCostRecords(ctx, normalized)
	} else {
		batch.Metadata.ProcessedAt = time.Now().UTC()
		batch.Metadata.RecordCount = len(batch.Records)
		batch.Metadata.Status = "processed"
		err = s.repo.StoreIngestedBatch(ctx, batch.Metadata, normalized)
	}
	if err != nil {
		if s.metrics != nil {
			s.metrics.ObserveFailure(provider, "store", 1)
		}
		if s.logger != nil {
			s.logger.Error("ingested batch store failed", "provider", provider, "object", batch.Metadata.ObjectName, "error", err)
		}
		return err
	}
	*recordsInserted += len(normalized)
	if s.logger != nil {
		s.logger.Info("records inserted", "provider", provider, "object", batch.Metadata.ObjectName, "records_inserted", len(normalized))
	}
	for _, rec := range normalized {
		if pr.EarliestRecord.IsZero() || rec.Timestamp.Before(pr.EarliestRecord) {
			pr.EarliestRecord = rec.Timestamp
		}
	}
	return nil
}
