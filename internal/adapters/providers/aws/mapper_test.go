package aws

import (
	"testing"
	"time"

	awsapi "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"

	"github.com/crypticani/torvix/internal/domain"
)

func TestMapTotalCostResponse(t *testing.T) {
	out := ceOutput(true, nil, []types.ResultByTime{{
		Estimated: true,
		TimePeriod: &types.DateInterval{
			Start: awsapi.String("2026-05-30"),
			End:   awsapi.String("2026-05-31"),
		},
		Total: metric("12.34", "USD"),
	}})

	records, estimated, err := mapCostAndUsageOutput(queryTotal, "UnblendedCost", out)
	if err != nil {
		t.Fatalf("mapCostAndUsageOutput() error = %v", err)
	}
	if estimated != 1 {
		t.Fatalf("estimated count = %d, want 1", estimated)
	}
	assertRecord(t, records, 0, wantRecord{
		Service:          "total",
		Region:           "global",
		BillingScopeType: "linked_account",
		BillingScopeID:   "all",
		Cost:             12.34,
		Currency:         "USD",
		QueryType:        "total",
		Estimated:        true,
	})
}

func TestMapGroupedCostResponses(t *testing.T) {
	tests := []struct {
		name      string
		queryType string
		keys      []string
		want      wantRecord
	}{
		{
			name:      "service",
			queryType: queryService,
			keys:      []string{"Amazon Simple Storage Service"},
			want:      wantRecord{Service: "Amazon Simple Storage Service", Region: "global", BillingScopeType: "linked_account", BillingScopeID: "all", QueryType: "service"},
		},
		{
			name:      "linked account",
			queryType: queryLinkedAccount,
			keys:      []string{"123456789012"},
			want:      wantRecord{Service: "total", Region: "global", BillingScopeType: "linked_account", BillingScopeID: "123456789012", BillingScopeName: "prod-account", QueryType: "linked_account"},
		},
		{
			name:      "linked account service",
			queryType: queryLinkedAccountService,
			keys:      []string{"123456789012", "Amazon Elastic Compute Cloud - Compute"},
			want:      wantRecord{Service: "Amazon Elastic Compute Cloud - Compute", Region: "global", BillingScopeType: "linked_account", BillingScopeID: "123456789012", BillingScopeName: "prod-account", QueryType: "linked_account_service"},
		},
		{
			name:      "region",
			queryType: queryRegion,
			keys:      []string{"ap-south-1"},
			want:      wantRecord{Service: "total", Region: "ap-south-1", BillingScopeType: "linked_account", BillingScopeID: "all", QueryType: "region"},
		},
		{
			name:      "region service",
			queryType: queryRegionService,
			keys:      []string{"ap-south-1", "Amazon Relational Database Service"},
			want:      wantRecord{Service: "Amazon Relational Database Service", Region: "ap-south-1", BillingScopeType: "linked_account", BillingScopeID: "all", QueryType: "region_service"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := ceOutput(false, []types.DimensionValuesWithAttributes{{
				Value: awsapi.String("123456789012"),
				Attributes: map[string]string{
					"description": "prod-account",
				},
			}}, []types.ResultByTime{{
				Estimated: false,
				TimePeriod: &types.DateInterval{
					Start: awsapi.String("2026-05-30"),
					End:   awsapi.String("2026-05-31"),
				},
				Groups: []types.Group{{Keys: tt.keys, Metrics: metric("7.50", "USD")}},
			}})

			records, estimated, err := mapCostAndUsageOutput(tt.queryType, "UnblendedCost", out)
			if err != nil {
				t.Fatalf("mapCostAndUsageOutput() error = %v", err)
			}
			if estimated != 0 {
				t.Fatalf("estimated count = %d, want 0", estimated)
			}
			tt.want.Cost = 7.50
			tt.want.Currency = "USD"
			assertRecord(t, records, 0, tt.want)
		})
	}
}

func TestMapEmptyRegionToGlobal(t *testing.T) {
	out := ceOutput(false, nil, []types.ResultByTime{{
		TimePeriod: &types.DateInterval{Start: awsapi.String("2026-05-30"), End: awsapi.String("2026-05-31")},
		Groups:     []types.Group{{Keys: []string{""}, Metrics: metric("1.00", "USD")}},
	}})

	records, _, err := mapCostAndUsageOutput(queryRegion, "UnblendedCost", out)
	if err != nil {
		t.Fatalf("mapCostAndUsageOutput() error = %v", err)
	}
	if records[0].Region != "global" {
		t.Fatalf("region = %q, want global", records[0].Region)
	}
}

func TestMapInvalidAmountReturnsError(t *testing.T) {
	out := ceOutput(false, nil, []types.ResultByTime{{
		TimePeriod: &types.DateInterval{Start: awsapi.String("2026-05-30"), End: awsapi.String("2026-05-31")},
		Total:      metric("not-a-number", "USD"),
	}})

	_, _, err := mapCostAndUsageOutput(queryTotal, "UnblendedCost", out)
	if err == nil {
		t.Fatal("expected invalid amount error")
	}
}

func TestMapEmptyResponse(t *testing.T) {
	records, estimated, err := mapCostAndUsageOutput(queryService, "UnblendedCost", &costexplorer.GetCostAndUsageOutput{})
	if err != nil {
		t.Fatalf("mapCostAndUsageOutput() error = %v", err)
	}
	if len(records) != 0 || estimated != 0 {
		t.Fatalf("records=%d estimated=%d, want 0/0", len(records), estimated)
	}
}

type wantRecord struct {
	Service          string
	Region           string
	BillingScopeType string
	BillingScopeID   string
	BillingScopeName string
	Cost             float64
	Currency         string
	QueryType        string
	Estimated        bool
}

func assertRecord(t *testing.T, records []domain.RawBillingRecord, index int, want wantRecord) {
	t.Helper()
	if len(records) <= index {
		t.Fatalf("record count = %d, want index %d", len(records), index)
	}
	got := records[index]
	if got.Provider != domain.ProviderAWS {
		t.Fatalf("provider = %q, want aws", got.Provider)
	}
	if !got.UsageStart.Equal(time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("usage start = %s, want 2026-05-30", got.UsageStart)
	}
	if got.Service != want.Service || got.Region != want.Region || got.Cost != want.Cost || got.Currency != want.Currency {
		t.Fatalf("record = %+v, want service=%q region=%q cost=%f currency=%q", got, want.Service, want.Region, want.Cost, want.Currency)
	}
	if got.BillingScopeType != want.BillingScopeType || got.BillingScopeID != want.BillingScopeID || got.BillingScopeName != want.BillingScopeName {
		t.Fatalf("billing scope = %q/%q/%q, want %q/%q/%q", got.BillingScopeType, got.BillingScopeID, got.BillingScopeName, want.BillingScopeType, want.BillingScopeID, want.BillingScopeName)
	}
	if got.RawData["query_type"] != want.QueryType || got.RawData["estimated"] != want.Estimated {
		t.Fatalf("raw metadata = %#v, want query_type=%q estimated=%v", got.RawData, want.QueryType, want.Estimated)
	}
	if got.RecordType != want.QueryType {
		t.Fatalf("record type = %q, want %q", got.RecordType, want.QueryType)
	}
}

func ceOutput(estimated bool, attrs []types.DimensionValuesWithAttributes, results []types.ResultByTime) *costexplorer.GetCostAndUsageOutput {
	return &costexplorer.GetCostAndUsageOutput{
		DimensionValueAttributes: attrs,
		ResultsByTime:            results,
	}
}

func metric(amount, unit string) map[string]types.MetricValue {
	return map[string]types.MetricValue{
		"UnblendedCost": {
			Amount: awsapi.String(amount),
			Unit:   awsapi.String(unit),
		},
	}
}
