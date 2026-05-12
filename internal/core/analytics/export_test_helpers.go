package analytics

import "github.com/crypticani/cloudpulse/internal/domain"

func TestDetectSeriesAnomalies(series []domain.AggregatedCost) []domain.Anomaly {
	return detectSeriesAnomalies(series)
}
