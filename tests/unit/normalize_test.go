package unit

import (
	"testing"
	"time"

	"github.com/crypticani/cloudpulse/internal/core/normalize"
	"github.com/crypticani/cloudpulse/internal/domain"
)

func TestNormalize(t *testing.T) {
	svc := normalize.New()
	raw := []domain.RawBillingRecord{
		{
			Provider:   domain.ProviderOCI,
			Service:    "COMPUTE",
			Category:   "Compute",
			UsageStart: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			Cost:       10.5,
		},
		{
			Provider:   domain.ProviderAWS,
			Service:    "AmazonEC2",
			UsageStart: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			Cost:       5.0,
		},
	}

	canon := svc.Normalize(raw)
	if len(canon) != 2 {
		t.Fatalf("expected 2 canonical records, got %d", len(canon))
	}
	if canon[0].Category != "Compute" {
		t.Errorf("expected Compute, got %s", canon[0].Category)
	}
	if canon[1].Category != "AmazonEC2" {
		t.Errorf("expected fallback AmazonEC2 category, got %s", canon[1].Category)
	}
}
