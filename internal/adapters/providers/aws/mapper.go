package aws

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"

	"github.com/crypticani/torvix/internal/domain"
)

const (
	queryTotal                = "total"
	queryService              = "service"
	queryLinkedAccount        = "linked_account"
	queryRegion               = "region"
	queryLinkedAccountService = "linked_account_service"
	queryRegionService        = "region_service"
)

type costQuery struct {
	Type     string
	GroupBy  []types.Dimension
	Optional bool
}

type queryResult struct {
	Records        []domain.RawBillingRecord
	EstimatedCount int
}

func mapCostAndUsageOutput(queryType, metric string, out *costexplorer.GetCostAndUsageOutput) ([]domain.RawBillingRecord, int, error) {
	if out == nil {
		return nil, 0, nil
	}
	accountNames := linkedAccountNames(out.DimensionValueAttributes)
	records := make([]domain.RawBillingRecord, 0)
	estimatedCount := 0
	for _, result := range out.ResultsByTime {
		day, err := resultDate(result.TimePeriod)
		if err != nil {
			return nil, estimatedCount, err
		}
		if len(result.Groups) == 0 {
			record, err := mapMetric(queryType, metric, day, nil, result.Total, result.Estimated, accountNames)
			if err != nil {
				return nil, estimatedCount, err
			}
			if record != nil {
				records = append(records, *record)
				if result.Estimated {
					estimatedCount++
				}
			}
			continue
		}
		for _, group := range result.Groups {
			record, err := mapMetric(queryType, metric, day, group.Keys, group.Metrics, result.Estimated, accountNames)
			if err != nil {
				return nil, estimatedCount, err
			}
			if record != nil {
				records = append(records, *record)
				if result.Estimated {
					estimatedCount++
				}
			}
		}
	}
	return records, estimatedCount, nil
}

func mapMetric(queryType, metric string, day time.Time, keys []string, values map[string]types.MetricValue, estimated bool, accountNames map[string]string) (*domain.RawBillingRecord, error) {
	value, ok := values[metric]
	if !ok {
		return nil, nil
	}
	amountRaw := strings.TrimSpace(stringValue(value.Amount))
	if amountRaw == "" {
		return nil, nil
	}
	amount, err := strconv.ParseFloat(amountRaw, 64)
	if err != nil {
		return nil, fmt.Errorf("parse AWS %s amount %q: %w", metric, amountRaw, err)
	}
	service := "total"
	region := "global"
	scopeID := "all"
	scopeName := ""
	switch queryType {
	case queryService:
		service = keyAt(keys, 0, "unknown")
	case queryLinkedAccount:
		scopeID = keyAt(keys, 0, "unknown")
		scopeName = accountNames[scopeID]
	case queryRegion:
		region = normalizeRegion(keyAt(keys, 0, ""))
	case queryLinkedAccountService:
		scopeID = keyAt(keys, 0, "unknown")
		scopeName = accountNames[scopeID]
		service = keyAt(keys, 1, "unknown")
	case queryRegionService:
		region = normalizeRegion(keyAt(keys, 0, ""))
		service = keyAt(keys, 1, "unknown")
	}
	raw := map[string]any{
		"query_type": queryType,
		"metric":     metric,
		"estimated":  estimated,
		"group_keys": append([]string(nil), keys...),
	}
	// TODO(aws-v2): populate project/network/resource fields from CUR 2.0, tags,
	// cost categories, inventory enrichment, and optional VPC-to-project maps.
	return &domain.RawBillingRecord{
		Provider:         domain.ProviderAWS,
		AccountID:        scopeID,
		UsageStart:       day,
		UsageEnd:         day.AddDate(0, 0, 1),
		Service:          service,
		Category:         service,
		Region:           region,
		BillingScopeType: "linked_account",
		BillingScopeID:   scopeID,
		BillingScopeName: scopeName,
		Currency:         stringValue(value.Unit),
		Cost:             amount,
		UsageAmount:      amount,
		UsageUnit:        stringValue(value.Unit),
		Tags: map[string]string{
			"billing_scope_type": "linked_account",
			"billing_scope_id":   scopeID,
			"billing_scope_name": scopeName,
		},
		Meter:        metric,
		RecordType:   queryType,
		RawData:      raw,
		SourceObject: "aws-cost-explorer",
	}, nil
}

func resultDate(interval *types.DateInterval) (time.Time, error) {
	if interval == nil || interval.Start == nil {
		return time.Time{}, fmt.Errorf("AWS Cost Explorer result missing start date")
	}
	t, err := time.Parse(time.DateOnly, *interval.Start)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse AWS result start date %q: %w", *interval.Start, err)
	}
	return t.UTC(), nil
}

func linkedAccountNames(attrs []types.DimensionValuesWithAttributes) map[string]string {
	out := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		if attr.Value == nil {
			continue
		}
		name := attr.Attributes["description"]
		if strings.TrimSpace(name) == "" {
			name = attr.Attributes["accountName"]
		}
		if strings.TrimSpace(name) != "" {
			out[*attr.Value] = name
		}
	}
	return out
}

func normalizeRegion(region string) string {
	region = strings.TrimSpace(region)
	if region == "" || strings.EqualFold(region, "NoRegion") {
		return "global"
	}
	return region
}

func keyAt(keys []string, index int, fallback string) string {
	if index >= len(keys) || strings.TrimSpace(keys[index]) == "" {
		return fallback
	}
	return strings.TrimSpace(keys[index])
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
