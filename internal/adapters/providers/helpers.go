package providers

import (
	"time"

	"github.com/crypticani/cloudpulse/internal/domain"
	"github.com/crypticani/cloudpulse/internal/ports/providers"
)

func Sample(provider domain.Provider, accountID, service string) providers.CollectResult {
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
			Provider:    provider,
			AccountID:   accountID,
			UsageStart:  day,
			UsageEnd:    day.Add(time.Hour),
			Service:     service,
			Category:    serviceCategory(service),
			SKU:         "standard",
			Region:      "global",
			ResourceID:  provider.String() + "-resource",
			Currency:    "USD",
			Cost:        cost,
			UsageAmount: 24,
			UsageUnit:   "hours",
			Tags:        map[string]string{"env": "demo"},
			Meter:       "standard",
			RawData: map[string]any{
				"provider": provider,
				"service":  service,
			},
			SourceObject: "bootstrap/sample",
		})
	}
	return providers.CollectResult{
		Batches: []providers.FileBatch{
			{
				Metadata: domain.ProcessedReportFile{
					Provider:     provider,
					Bucket:       "sample",
					ObjectName:   "bootstrap/sample",
					ETag:         "sample-etag",
					LastModified: now,
				},
				Records: out,
			},
		},
		FilesProcessed:   1,
		RecordsProcessed: len(out),
	}
}

func serviceCategory(service string) string {
	switch service {
	case "AmazonEC2", "Virtual Machines", "Compute Engine":
		return "Compute"
	default:
		return service
	}
}
