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
	logger      *slog.Logger
	repo        storage.Repository
	normalizer  *normalize.Service
	collectors  []providers.Collector
}

func New(logger *slog.Logger, repo storage.Repository, normalizer *normalize.Service, collectors []providers.Collector) *Service {
	return &Service{logger: logger, repo: repo, normalizer: normalizer, collectors: collectors}
}

func (s *Service) Run(ctx context.Context, since time.Time) error {
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		allRaw  []domain.RawBillingRecord
		allErrs []error
	)

	for _, collector := range s.collectors {
		c := collector
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.logger.Info("collector start", "provider", c.Name(), "since", since)
			records, err := c.Collect(ctx, since)
			if err != nil {
				mu.Lock()
				allErrs = append(allErrs, err)
				mu.Unlock()
				s.logger.Error("collector failed", "provider", c.Name(), "error", err)
				return
			}
			mu.Lock()
			allRaw = append(allRaw, records...)
			mu.Unlock()
			s.logger.Info("collector done", "provider", c.Name(), "records", len(records))
		}()
	}

	wg.Wait()
	normalized := s.normalizer.Normalize(allRaw)
	if len(normalized) > 0 {
		if err := s.repo.InsertCanonical(ctx, normalized); err != nil {
			allErrs = append(allErrs, err)
		}
	}

	return errors.Join(allErrs...)
}
