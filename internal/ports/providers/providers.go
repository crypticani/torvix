package providers

import (
	"context"
	"time"

	"github.com/crypticani/cloudpulse/internal/domain"
)

type FileBatch struct {
	Metadata domain.ProcessedReportFile
	Records  []domain.RawBillingRecord
}

type CollectResult struct {
	Batches          []FileBatch
	FilesProcessed   int
	FilesSkipped     int
	RecordsProcessed int
	Failures         int
}

type Collector interface {
	Name() string
	Collect(ctx context.Context, since time.Time) (CollectResult, error)
}
