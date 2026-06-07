package aws

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/crypticani/torvix/internal/config"
	"github.com/crypticani/torvix/internal/domain"
	"github.com/crypticani/torvix/internal/ports/providers"
)

func TestCURParserMapsCSVHeaderVariants(t *testing.T) {
	input := strings.NewReader(strings.Join([]string{
		"line_item_usage_start_date,line_item_usage_account_id,product_product_name,product_region,line_item_unblended_cost,pricing_currency,line_item_usage_amount,pricing_unit,line_item_resource_id,line_item_usage_type,line_item_operation,line_item_line_item_type,resource_tags_user_project,resourceTags/user:Environment,product_product_family",
		"2026-05-30T12:34:56Z,123456789012,Amazon Elastic Compute Cloud,ap-south-1,42.25,USD,7.5,Hrs,i-1234567890abcdef0,BoxUsage:m5.large,RunInstances,Usage,torvix-api,prod,Compute Instance",
	}, "\n"))

	result, err := parseCURCSV(input, curSource{Key: "exports/cur.csv", ETag: "etag-1", Format: "csv"})
	if err != nil {
		t.Fatalf("parseCURCSV() error = %v", err)
	}
	if result.RowsParsed != 1 || result.RowsSkipped != 0 || result.MalformedRows != 0 {
		t.Fatalf("unexpected parse counters: %+v", result)
	}
	if len(result.Records) != 1 {
		t.Fatalf("expected one record, got %d", len(result.Records))
	}
	got := result.Records[0]
	if got.Provider != domain.ProviderAWS || got.RecordType != "cur_line_item" {
		t.Fatalf("provider/record type = %q/%q, want aws/cur_line_item", got.Provider, got.RecordType)
	}
	if got.BillingScopeType != "linked_account" || got.BillingScopeID != "123456789012" || got.BillingScopeName != "123456789012" {
		t.Fatalf("billing scope = %q/%q/%q", got.BillingScopeType, got.BillingScopeID, got.BillingScopeName)
	}
	if got.Service != "Amazon Elastic Compute Cloud" || got.Region != "ap-south-1" || got.Cost != 42.25 || got.Currency != "USD" {
		t.Fatalf("service/region/cost/currency mismatch: %+v", got)
	}
	if got.UsageAmount != 7.5 || got.UsageUnit != "Hrs" || got.ResourceID != "i-1234567890abcdef0" {
		t.Fatalf("usage/resource mismatch: %+v", got)
	}
	if got.ProjectSource != "tag" || got.ProjectName != "torvix-api" || got.ProjectID != "torvix-api" {
		t.Fatalf("project tag not mapped: %+v", got)
	}
	if got.Tags["Project"] != "torvix-api" || got.Tags["Environment"] != "prod" {
		t.Fatalf("tags not parsed: %+v", got.Tags)
	}
	if got.RawData["line_item_usage_type"] != "BoxUsage:m5.large" || got.RawData["line_item_operation"] != "RunInstances" || got.RawData["product_product_family"] != "Compute Instance" {
		t.Fatalf("raw metadata missing expected fields: %#v", got.RawData)
	}
	if got.SourceFileKey != "exports/cur.csv" || got.SourceFileETag != "etag-1" || got.SourceLineNumber != 2 || got.SourceRecordHash == "" {
		t.Fatalf("source tracking not populated: key=%q etag=%q line=%d hash=%q", got.SourceFileKey, got.SourceFileETag, got.SourceLineNumber, got.SourceRecordHash)
	}
}

func TestCURParserSupportsSlashHeaderVariantsAndGlobalRegion(t *testing.T) {
	input := strings.NewReader(strings.Join([]string{
		"lineItem/UsageStartDate,lineItem/UsageAccountId,lineItem/ProductCode,lineItem/UnblendedCost,pricing/Currency,lineItem/UsageAmount,lineItem/ResourceId,lineItem/UsageType,lineItem/Operation,resourceTags/user:Project",
		"2026-05-30 00:00:00,210987654321,AmazonS3,3.14,USD,2.0,bucket/example,TimedStorage-ByteHrs,StandardStorage,analytics",
	}, "\n"))

	result, err := parseCURCSV(input, curSource{Key: "exports/slash.csv", ETag: "etag-2", Format: "csv"})
	if err != nil {
		t.Fatalf("parseCURCSV() error = %v", err)
	}
	got := result.Records[0]
	if got.Service != "AmazonS3" || got.Region != "global" {
		t.Fatalf("service/region = %q/%q, want AmazonS3/global", got.Service, got.Region)
	}
	if got.ProjectName != "analytics" || got.Tags["Project"] != "analytics" {
		t.Fatalf("project tag not mapped from slash header: %+v tags=%+v", got, got.Tags)
	}
}

func TestCURParserSkipsInvalidCostAndDateRows(t *testing.T) {
	input := strings.NewReader(strings.Join([]string{
		"line_item_usage_start_date,line_item_usage_account_id,product_product_name,line_item_unblended_cost",
		"2026-05-30T00:00:00Z,123456789012,Amazon S3,not-a-number",
		"not-a-date,123456789012,Amazon EC2,1.00",
		"2026-05-31T00:00:00Z,123456789012,Amazon RDS,2.50",
	}, "\n"))

	result, err := parseCURCSV(input, curSource{Key: "exports/bad.csv", ETag: "etag-3", Format: "csv"})
	if err != nil {
		t.Fatalf("parseCURCSV() error = %v", err)
	}
	if len(result.Records) != 1 || result.RowsSkipped != 2 || result.MalformedRows != 2 {
		t.Fatalf("unexpected parse result: %+v", result)
	}
	if result.Records[0].Service != "Amazon RDS" {
		t.Fatalf("unexpected retained record: %+v", result.Records[0])
	}
}

func TestCURRecordHashIsDeterministic(t *testing.T) {
	input := strings.NewReader(strings.Join([]string{
		"line_item_usage_start_date,line_item_usage_account_id,product_product_name,line_item_unblended_cost,line_item_resource_id,line_item_usage_type,line_item_operation,line_item_line_item_type",
		"2026-05-30T00:00:00Z,123456789012,Amazon S3,1.00,bucket/example,TimedStorage,StandardStorage,Usage",
	}, "\n"))

	first, err := parseCURCSV(input, curSource{Key: "exports/cur.csv", ETag: "etag-1", Format: "csv"})
	if err != nil {
		t.Fatalf("first parse: %v", err)
	}
	second, err := parseCURCSV(strings.NewReader(strings.Join([]string{
		"line_item_usage_start_date,line_item_usage_account_id,product_product_name,line_item_unblended_cost,line_item_resource_id,line_item_usage_type,line_item_operation,line_item_line_item_type",
		"2026-05-30T00:00:00Z,123456789012,Amazon S3,1.00,bucket/example,TimedStorage,StandardStorage,Usage",
	}, "\n")), curSource{Key: "exports/regenerated/cur.csv", ETag: "etag-2", Format: "csv"})
	if err != nil {
		t.Fatalf("second parse: %v", err)
	}
	if first.Records[0].SourceRecordHash == "" || first.Records[0].SourceRecordHash != second.Records[0].SourceRecordHash {
		t.Fatalf("hash should be deterministic across regenerated file keys and etag changes: %q vs %q", first.Records[0].SourceRecordHash, second.Records[0].SourceRecordHash)
	}
}

func TestCURCollectorReadsLocalGzipFile(t *testing.T) {
	path := writeGzipCURFixture(t, strings.Join([]string{
		"line_item_usage_start_date,line_item_usage_account_id,product_product_name,product_region,line_item_unblended_cost,pricing_currency",
		"2026-05-30T00:00:00Z,123456789012,Amazon S3,us-east-1,9.99,USD",
	}, "\n"))

	collector := NewCURCollector(config.AWSProvider{
		Enabled:       true,
		IngestionMode: "cur_s3",
		CURLocalPath:  path,
		CURFormat:     "csv_gzip",
	}, nil, nil)

	result, err := collector.Collect(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if result.FilesProcessed != 1 || result.RecordsProcessed != 1 || len(result.Batches) != 1 {
		t.Fatalf("unexpected local CUR collect result: %+v", result)
	}
	batch := result.Batches[0]
	if batch.Metadata.Provider != domain.ProviderAWS || batch.Metadata.Bucket != "local_file" || batch.Metadata.ObjectName != path {
		t.Fatalf("unexpected batch metadata: %+v", batch.Metadata)
	}
	if batch.Records[0].SourceFileKey != path || batch.Records[0].Cost != 9.99 {
		t.Fatalf("unexpected local record: %+v", batch.Records[0])
	}
}

func TestCURCollectorCollectsSelectedS3Objects(t *testing.T) {
	body := gzipCURFixture(t, strings.Join([]string{
		"line_item_usage_start_date,line_item_usage_account_id,product_product_name,product_region,line_item_unblended_cost,pricing_currency",
		"2026-05-30T00:00:00Z,123456789012,Amazon S3,us-east-1,9.99,USD",
	}, "\n"))
	now := time.Now().UTC()
	client := &fakeS3Client{
		listPages: []*s3.ListObjectsV2Output{
			{
				Contents: []s3types.Object{
					{Key: aws.String("billing/readme.txt"), LastModified: aws.Time(now), Size: aws.Int64(1)},
					{Key: aws.String("billing/old.csv.gz"), ETag: aws.String(`"old-etag"`), LastModified: aws.Time(now.AddDate(0, 0, -10)), Size: aws.Int64(10)},
				},
				NextContinuationToken: aws.String("next"),
			},
			{
				Contents: []s3types.Object{
					{Key: aws.String("billing/current.csv.gz"), ETag: aws.String(`"new-etag"`), LastModified: aws.Time(now), Size: aws.Int64(int64(len(body)))},
				},
			},
		},
		objects: map[string][]byte{"billing/current.csv.gz": body},
	}
	collector := NewCURCollector(config.AWSProvider{
		Enabled:         true,
		IngestionMode:   "cur_s3",
		CURBucket:       "billing-bucket",
		CURPrefix:       "billing/",
		CURFormat:       "csv_gzip",
		CURLookbackDays: 3,
	}, nil, client)

	result, err := collector.Collect(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if client.listCalls != 2 || client.getCalls != 1 {
		t.Fatalf("expected paginated list and one download, got list=%d get=%d", client.listCalls, client.getCalls)
	}
	if result.FilesProcessed != 1 || result.RecordsProcessed != 1 || len(result.Batches) != 1 {
		t.Fatalf("unexpected S3 collect result: %+v", result)
	}
	batch := result.Batches[0]
	if batch.Metadata.Bucket != "billing-bucket" || batch.Metadata.ObjectName != "billing/current.csv.gz" || batch.Metadata.ETag != "new-etag" {
		t.Fatalf("unexpected S3 metadata: %+v", batch.Metadata)
	}
	if batch.Records[0].SourceFileKey != "billing/current.csv.gz" || batch.Records[0].SourceFileETag != "new-etag" || batch.Records[0].Cost != 9.99 {
		t.Fatalf("unexpected S3 record: %+v", batch.Records[0])
	}
}

func TestCURCollectorStreamsBoundedBatches(t *testing.T) {
	path := writeGzipCURFixture(t, strings.Join([]string{
		"line_item_usage_start_date,line_item_usage_account_id,product_product_name,product_region,line_item_unblended_cost,pricing_currency",
		"2026-05-30T00:00:00Z,123456789012,Amazon S3,us-east-1,1.00,USD",
		"2026-05-30T01:00:00Z,123456789012,Amazon S3,us-east-1,2.00,USD",
		"2026-05-30T02:00:00Z,123456789012,Amazon S3,us-east-1,3.00,USD",
		"2026-05-30T03:00:00Z,123456789012,Amazon S3,us-east-1,4.00,USD",
		"2026-05-30T04:00:00Z,123456789012,Amazon S3,us-east-1,5.00,USD",
	}, "\n"))

	collector := NewCURCollector(config.AWSProvider{
		Enabled:            true,
		IngestionMode:      "cur_s3",
		CURLocalPath:       path,
		CURFormat:          "csv_gzip",
		MaxRecordsPerBatch: 2,
	}, nil, nil)

	var batchSizes []int
	var finalBatches int
	result, err := collector.CollectStream(context.Background(), time.Time{}, func(_ context.Context, batch providers.FileBatch) error {
		if batch.Final {
			finalBatches++
			if batch.Metadata.RecordCount != 5 {
				t.Fatalf("final record count = %d, want 5", batch.Metadata.RecordCount)
			}
			return nil
		}
		batchSizes = append(batchSizes, len(batch.Records))
		if len(batch.Records) > 2 {
			t.Fatalf("batch exceeded configured limit: %d", len(batch.Records))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("CollectStream() error = %v", err)
	}
	if len(result.Batches) != 0 {
		t.Fatalf("streaming collector must not retain batches in memory: %d", len(result.Batches))
	}
	if result.FilesProcessed != 1 || result.RecordsProcessed != 5 || result.BatchesInserted != 3 {
		t.Fatalf("unexpected streaming result: %+v", result)
	}
	if !slices.Equal(batchSizes, []int{2, 2, 1}) {
		t.Fatalf("batch sizes = %v, want [2 2 1]", batchSizes)
	}
	if finalBatches != 1 {
		t.Fatalf("final batches = %d, want 1", finalBatches)
	}
}

func TestCURCollectorCleansPartialSourceWhenHandlerFails(t *testing.T) {
	path := writeGzipCURFixture(t, strings.Join([]string{
		"line_item_usage_start_date,line_item_usage_account_id,product_product_name,product_region,line_item_unblended_cost,pricing_currency",
		"2026-05-30T00:00:00Z,123456789012,Amazon S3,us-east-1,1.00,USD",
		"2026-05-30T01:00:00Z,123456789012,Amazon S3,us-east-1,2.00,USD",
		"2026-05-30T02:00:00Z,123456789012,Amazon S3,us-east-1,3.00,USD",
	}, "\n"))
	cleaner := &fakeSourceCleaner{}
	collector := NewCURCollector(config.AWSProvider{
		Enabled:            true,
		IngestionMode:      "cur_s3",
		CURLocalPath:       path,
		CURFormat:          "csv_gzip",
		MaxRecordsPerBatch: 1,
	}, nil, nil, cleaner)

	calls := 0
	_, err := collector.CollectStream(context.Background(), time.Time{}, func(_ context.Context, batch providers.FileBatch) error {
		if batch.Final {
			return nil
		}
		calls++
		if calls == 2 {
			return io.ErrUnexpectedEOF
		}
		return nil
	})
	if err == nil {
		t.Fatal("CollectStream() error = nil, want handler failure")
	}
	if !slices.Equal(cleaner.sources, []string{path}) {
		t.Fatalf("cleaned sources = %v, want [%s]", cleaner.sources, path)
	}
}

type fakeSourceCleaner struct {
	sources []string
}

func (f *fakeSourceCleaner) DeleteCostRecordsForSource(_ context.Context, provider domain.Provider, sourceObject string) error {
	if provider != domain.ProviderAWS {
		return io.ErrUnexpectedEOF
	}
	f.sources = append(f.sources, sourceObject)
	return nil
}

func writeGzipCURFixture(t *testing.T, content string) string {
	t.Helper()
	buf := gzipCURFixture(t, content)
	path := t.TempDir() + "/cur-sample.csv.gz"
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
	return path
}

func gzipCURFixture(t *testing.T, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write([]byte(content)); err != nil {
		t.Fatalf("write gzip fixture: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip fixture: %v", err)
	}
	return buf.Bytes()
}

type fakeS3Client struct {
	listPages []*s3.ListObjectsV2Output
	objects   map[string][]byte
	listCalls int
	getCalls  int
}

func (f *fakeS3Client) ListObjectsV2(ctx context.Context, input *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	f.listCalls++
	if len(f.listPages) == 0 {
		return &s3.ListObjectsV2Output{}, nil
	}
	out := f.listPages[0]
	f.listPages = f.listPages[1:]
	return out, nil
}

func (f *fakeS3Client) GetObject(ctx context.Context, input *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	f.getCalls++
	body := f.objects[aws.ToString(input.Key)]
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(body))}, nil
}
