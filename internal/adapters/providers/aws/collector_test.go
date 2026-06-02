package aws

import (
	"context"
	"errors"
	"testing"
	"time"

	awsapi "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"

	"github.com/crypticani/torvix/internal/config"
)

func TestCollectorPaginatesCostExplorerQueries(t *testing.T) {
	client := &fakeCostExplorerClient{
		pages: map[string][]*costexplorer.GetCostAndUsageOutput{
			queryService: {
				{
					NextPageToken: awsapi.String("next"),
					ResultsByTime: []types.ResultByTime{{
						TimePeriod: &types.DateInterval{Start: awsapi.String("2026-05-30"), End: awsapi.String("2026-05-31")},
						Groups:     []types.Group{{Keys: []string{"Amazon S3"}, Metrics: metric("1.00", "USD")}},
					}},
				},
				{
					ResultsByTime: []types.ResultByTime{{
						TimePeriod: &types.DateInterval{Start: awsapi.String("2026-05-30"), End: awsapi.String("2026-05-31")},
						Groups:     []types.Group{{Keys: []string{"Amazon EC2"}, Metrics: metric("2.00", "USD")}},
					}},
				},
			},
		},
	}
	collector := NewWithClient(config.AWSProvider{Enabled: true, CostMetric: "UnblendedCost", Region: "us-east-1", LookbackDays: 3, ReportLagDays: 2}, nil, client)

	result, err := collector.collectQuery(context.Background(), costQuery{Type: queryService, GroupBy: []types.Dimension{types.DimensionService}}, time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("collectQuery() error = %v", err)
	}
	if len(result.Records) != 2 {
		t.Fatalf("record count = %d, want 2", len(result.Records))
	}
	if len(client.requests) != 2 {
		t.Fatalf("request count = %d, want 2", len(client.requests))
	}
	if client.requests[1].NextPageToken == nil || *client.requests[1].NextPageToken != "next" {
		t.Fatalf("second page token = %#v, want next", client.requests[1].NextPageToken)
	}
}

func TestCollectorOptionalRegionFailureDoesNotFailCollection(t *testing.T) {
	client := &fakeCostExplorerClient{
		pages: map[string][]*costexplorer.GetCostAndUsageOutput{
			queryTotal: {
				{ResultsByTime: []types.ResultByTime{{TimePeriod: &types.DateInterval{Start: awsapi.String("2026-05-30"), End: awsapi.String("2026-05-31")}, Total: metric("5.00", "USD")}}},
			},
			queryService: {
				{ResultsByTime: []types.ResultByTime{{TimePeriod: &types.DateInterval{Start: awsapi.String("2026-05-30"), End: awsapi.String("2026-05-31")}, Groups: []types.Group{{Keys: []string{"Amazon S3"}, Metrics: metric("1.00", "USD")}}}}},
			},
			queryLinkedAccount: {
				{ResultsByTime: []types.ResultByTime{{TimePeriod: &types.DateInterval{Start: awsapi.String("2026-05-30"), End: awsapi.String("2026-05-31")}, Groups: []types.Group{{Keys: []string{"123456789012"}, Metrics: metric("1.00", "USD")}}}}},
			},
			queryLinkedAccountService: {
				{ResultsByTime: []types.ResultByTime{{TimePeriod: &types.DateInterval{Start: awsapi.String("2026-05-30"), End: awsapi.String("2026-05-31")}, Groups: []types.Group{{Keys: []string{"123456789012", "Amazon S3"}, Metrics: metric("1.00", "USD")}}}}},
			},
		},
		errs: map[string]error{
			queryRegion:        errors.New("grouping REGION is unsupported"),
			queryRegionService: errors.New("grouping REGION is unsupported"),
		},
	}
	collector := NewWithClient(config.AWSProvider{Enabled: true, CostMetric: "UnblendedCost", Region: "us-east-1", LookbackDays: 3, ReportLagDays: 2}, nil, client)

	result, err := collector.Collect(context.Background(), time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if result.RecordsProcessed != 4 {
		t.Fatalf("records processed = %d, want 4", result.RecordsProcessed)
	}
	if result.Failures != 2 {
		t.Fatalf("failures = %d, want 2 optional region failures", result.Failures)
	}
	if len(result.Batches) != 1 {
		t.Fatalf("batches = %d, want 1", len(result.Batches))
	}
}

func TestNewDefaultsToCURWithoutCostExplorerClientSetup(t *testing.T) {
	collector, err := New(context.Background(), config.AWSProvider{Enabled: true}, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, ok := collector.(*CURCollector); !ok {
		t.Fatalf("default AWS collector = %T, want *CURCollector", collector)
	}

	_, err = collector.Collect(context.Background(), time.Time{})
	if err == nil {
		t.Fatal("expected missing CUR source config error")
	}
	if got := err.Error(); got != "AWS CUR ingestion requires TORVIX_AWS_CUR_BUCKET or TORVIX_AWS_CUR_LOCAL_PATH" {
		t.Fatalf("unexpected missing CUR config error %q", got)
	}
}

func TestNewRejectsUnknownAWSIngestionMode(t *testing.T) {
	_, err := New(context.Background(), config.AWSProvider{Enabled: true, IngestionMode: "bad-mode"}, nil)
	if err == nil {
		t.Fatal("expected unknown ingestion mode error")
	}
}

type fakeCostExplorerClient struct {
	pages    map[string][]*costexplorer.GetCostAndUsageOutput
	errs     map[string]error
	requests []*costexplorer.GetCostAndUsageInput
	calls    map[string]int
}

func (f *fakeCostExplorerClient) GetCostAndUsage(ctx context.Context, input *costexplorer.GetCostAndUsageInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error) {
	f.requests = append(f.requests, input)
	queryType := queryTypeFromInput(input)
	if f.errs != nil && f.errs[queryType] != nil {
		return nil, f.errs[queryType]
	}
	if f.calls == nil {
		f.calls = make(map[string]int)
	}
	index := f.calls[queryType]
	f.calls[queryType]++
	pages := f.pages[queryType]
	if index >= len(pages) {
		return &costexplorer.GetCostAndUsageOutput{}, nil
	}
	return pages[index], nil
}

func queryTypeFromInput(input *costexplorer.GetCostAndUsageInput) string {
	if len(input.GroupBy) == 0 {
		return queryTotal
	}
	keys := make([]string, 0, len(input.GroupBy))
	for _, group := range input.GroupBy {
		if group.Key != nil {
			keys = append(keys, *group.Key)
		}
	}
	switch {
	case len(keys) == 1 && keys[0] == string(types.DimensionService):
		return queryService
	case len(keys) == 1 && keys[0] == string(types.DimensionLinkedAccount):
		return queryLinkedAccount
	case len(keys) == 1 && keys[0] == string(types.DimensionRegion):
		return queryRegion
	case len(keys) == 2 && keys[0] == string(types.DimensionLinkedAccount):
		return queryLinkedAccountService
	case len(keys) == 2 && keys[0] == string(types.DimensionRegion):
		return queryRegionService
	default:
		return "unknown"
	}
}
