package providers

import (
	"context"
	"time"

	"github.com/crypticani/torvix/internal/domain"
)

type FileBatch struct {
	Metadata domain.ProcessedReportFile
	Records  []domain.RawBillingRecord
	First    bool
	Final    bool
}

type CollectResult struct {
	Batches           []FileBatch
	FilesProcessed    int
	FilesSkipped      int
	SkippedOldFiles   int
	RecordsProcessed  int
	BatchesInserted   int
	HitFileLimit      bool
	HitRuntimeLimit   bool
	HitZeroYieldLimit bool
	Failures          int
}

type Collector interface {
	Name() string
	Collect(ctx context.Context, since time.Time) (CollectResult, error)
}

type BatchHandler func(ctx context.Context, batch FileBatch) error

type StreamCollector interface {
	Collector
	CollectStream(ctx context.Context, since time.Time, handle BatchHandler) (CollectResult, error)
}
