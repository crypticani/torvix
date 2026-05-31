package analytics

import "github.com/crypticani/torvix/internal/domain"

func TestDetectSeriesAnomalies(series []domain.AggregatedCost) []domain.Anomaly {
	return detectSeriesAnomalies(series)
}
