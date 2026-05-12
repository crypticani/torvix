package providers

import (
	"context"
	"time"

	"github.com/crypticani/cloudpulse/internal/domain"
)

type Collector interface {
	Name() string
	Collect(ctx context.Context, since time.Time) ([]domain.RawBillingRecord, error)
}
