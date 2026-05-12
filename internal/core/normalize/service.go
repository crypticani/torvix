package normalize

import (
	"time"

	"github.com/crypticani/cloudpulse/internal/domain"
)

type Service struct{}

func New() *Service {
	return &Service{}
}

func (s *Service) Normalize(raw []domain.RawBillingRecord) []domain.CanonicalCostRecord {
	out := make([]domain.CanonicalCostRecord, 0, len(raw))
	for _, r := range raw {
		out = append(out, domain.CanonicalCostRecord{
			Date:         startOfDay(r.UsageStart),
			Provider:     r.Provider,
			AccountID:    r.AccountID,
			Service:      r.Service,
			Region:       r.Region,
			ResourceID:   r.ResourceID,
			Currency:     r.Currency,
			Cost:         r.Cost,
			UsageAmount:  r.UsageAmount,
			UsageUnit:    r.UsageUnit,
			Tags:         r.Tags,
			SourceObject: r.SourceObject,
		})
	}
	return out
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
