package unit

import (
	"testing"
	"time"

	"github.com/crypticani/cloudpulse/internal/core/analytics"
	"github.com/crypticani/cloudpulse/internal/domain"
)

func TestDetectSeriesAnomalies(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	var series []domain.AggregatedCost
	for i := 0; i < 7; i++ {
		cost := 100.0
		if i == 6 {
			cost = 250
		}
		series = append(series, domain.AggregatedCost{
			WindowStart: base.AddDate(0, 0, i),
			Provider:    domain.ProviderOCI,
			AccountID:   "acct",
			Service:     "compute",
			TotalCost:   cost,
		})
	}

	out := analytics.TestDetectSeriesAnomalies(series)
	if len(out) != 1 {
		t.Fatalf("expected 1 anomaly, got %d", len(out))
	}
	if out[0].Severity != "high" {
		t.Fatalf("expected high severity, got %s", out[0].Severity)
	}
}
