package collect

import "time"

type MetricsRecorder interface {
	ObserveCollector(provider, status string, duration time.Duration)
	ObserveFiles(provider, status string, count int)
	ObserveRecords(provider string, count int)
	ObserveBatches(provider string, count int)
	ObserveRecordsPerSecond(provider string, rate float64)
	ObserveRecordsDeleted(count int64)
	ObserveCompressedChunks(count int64)
	ObserveFailure(provider, stage string, count int)
}
