package providers

import (
	"time"

	"github.com/crypticani/cloudpulse/internal/domain"
)

func Sample(provider domain.Provider, accountID, service string) []domain.RawBillingRecord {
	if accountID == "" {
		accountID = "demo-account"
	}
	now := time.Now().UTC()
	var out []domain.RawBillingRecord
	for i := 0; i < 14; i++ {
		day := now.AddDate(0, 0, -i)
		cost := 100.0
		if i == 0 {
			cost = 180
		}
		out = append(out, domain.RawBillingRecord{
			Provider:     provider,
			AccountID:    accountID,
			UsageStart:   day,
			UsageEnd:     day.Add(time.Hour),
			Service:      service,
			SKU:          "standard",
			Region:       "global",
			ResourceID:   provider.String() + "-resource",
			Currency:     "USD",
			Cost:         cost,
			UsageAmount:  24,
			UsageUnit:    "hours",
			Tags:         map[string]string{"env": "demo"},
			SourceObject: "bootstrap/sample",
		})
	}
	return out
}
