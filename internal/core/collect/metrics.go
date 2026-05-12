package collect

import "time"

type MetricsRecorder interface {
	ObserveCollector(provider, status string, duration time.Duration)
	ObserveFiles(provider, status string, count int)
	ObserveRecords(provider string, count int)
	ObserveFailure(provider, stage string, count int)
}
