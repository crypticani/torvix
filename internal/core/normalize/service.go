package normalize

import "github.com/crypticani/torvix/internal/domain"

type Service struct{}

func New() *Service {
	return &Service{}
}

func (s *Service) Normalize(raw []domain.RawBillingRecord) []domain.CanonicalCostRecord {
	out := make([]domain.CanonicalCostRecord, 0, len(raw))
	for _, r := range raw {
		out = append(out, domain.CanonicalCostRecord{
			Timestamp:    r.UsageStart.UTC(),
			Provider:     r.Provider,
			AccountID:    r.AccountID,
			Service:      r.Service,
			Category:     defaultCategory(r),
			Region:       r.Region,
			ResourceID:   r.ResourceID,
			Currency:     r.Currency,
			Cost:         r.Cost,
			UsageAmount:  r.UsageAmount,
			UsageUnit:    r.UsageUnit,
			Tags:         r.Tags,
			Meter:        r.Meter,
			RawData:      r.RawData,
			SourceObject: r.SourceObject,
		})
	}
	return out
}

func defaultCategory(r domain.RawBillingRecord) string {
	if r.Category != "" {
		return r.Category
	}
	return r.Service
}
