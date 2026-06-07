package aws

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/crypticani/torvix/internal/config"
	"github.com/crypticani/torvix/internal/domain"
	"github.com/crypticani/torvix/internal/ports/providers"
)

type sourceCleaner interface {
	DeleteCostRecordsForSource(ctx context.Context, provider domain.Provider, sourceObject string) error
}

type CURCollector struct {
	cfg    config.AWSProvider
	logger *slog.Logger
	client S3Client
	repo   sourceCleaner
}

type curObject struct {
	Key          string
	ETag         string
	LastModified time.Time
	Size         int64
}

func NewCURCollector(cfg config.AWSProvider, logger *slog.Logger, client S3Client, cleaners ...sourceCleaner) *CURCollector {
	if logger == nil {
		logger = slog.Default()
	}
	var repo sourceCleaner
	if len(cleaners) > 0 {
		repo = cleaners[0]
	}
	return &CURCollector{cfg: cfg.WithDefaults(), logger: logger, client: client, repo: repo}
}

func (c *CURCollector) Name() string { return string(domain.ProviderAWS) }

func (c *CURCollector) Collect(ctx context.Context, since time.Time) (providers.CollectResult, error) {
	var (
		batches       []providers.FileBatch
		records       []domain.RawBillingRecord
		currentObject string
	)
	result, err := c.CollectStream(ctx, since, func(_ context.Context, batch providers.FileBatch) error {
		if !batch.Final {
			if currentObject != "" && currentObject != batch.Metadata.ObjectName {
				records = nil
			}
			currentObject = batch.Metadata.ObjectName
			records = append(records, batch.Records...)
			return nil
		}
		if currentObject != "" && currentObject != batch.Metadata.ObjectName {
			records = nil
		}
		batches = append(batches, providers.FileBatch{
			Metadata: batch.Metadata,
			Records:  records,
		})
		records = nil
		currentObject = ""
		return nil
	})
	result.Batches = batches
	return result, err
}

func (c *CURCollector) CollectStream(ctx context.Context, _ time.Time, handle providers.BatchHandler) (providers.CollectResult, error) {
	if !c.cfg.Enabled {
		c.logger.Info("AWS CUR collector skipped because provider is disabled", "provider", domain.ProviderAWS, "ingestion_mode", "cur_s3")
		return providers.CollectResult{}, nil
	}
	if strings.TrimSpace(c.cfg.CURLocalPath) != "" {
		return c.collectLocalFileStream(ctx, c.cfg.CURLocalPath, handle)
	}
	if strings.TrimSpace(c.cfg.CURBucket) == "" {
		return providers.CollectResult{}, fmt.Errorf("AWS CUR ingestion requires %s or %s", config.EnvAWSCURBucket, config.EnvAWSCURLocalPath)
	}
	if strings.TrimSpace(c.cfg.CURPrefix) == "" {
		return providers.CollectResult{}, fmt.Errorf("AWS CUR ingestion requires %s when local path is not set", config.EnvAWSCURPrefix)
	}
	client := c.client
	if client == nil {
		var err error
		client, err = NewS3Client(ctx, c.cfg)
		if err != nil {
			return providers.CollectResult{}, fmt.Errorf("create AWS S3 CUR client: %w", err)
		}
	}
	objects, err := c.listCURObjects(ctx, client)
	if err != nil {
		return providers.CollectResult{}, err
	}
	if len(objects) == 0 {
		c.logger.Info("no AWS CUR billing export files found", "provider", domain.ProviderAWS, "ingestion_mode", "cur_s3", "bucket", c.cfg.CURBucket, "prefix", c.cfg.CURPrefix)
		return providers.CollectResult{}, nil
	}
	limits := c.cfg.IngestionLimits()
	deadline := time.Now().Add(limits.MaxRuntime)
	var result providers.CollectResult
	var errs []error
	for index, object := range objects {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		if index >= limits.MaxFilesPerRun {
			result.HitFileLimit = true
			c.logger.Warn("AWS CUR max files per run reached", "provider", domain.ProviderAWS, "limit", limits.MaxFilesPerRun)
			break
		}
		if index > 0 && time.Now().After(deadline) {
			result.HitRuntimeLimit = true
			c.logger.Warn("AWS CUR max runtime reached before starting next object", "provider", domain.ProviderAWS, "limit", limits.MaxRuntime)
			break
		}
		parsed, err := c.collectS3ObjectStream(ctx, client, object, handle, &result)
		if err != nil {
			result.Failures++
			errs = append(errs, err)
			c.logger.Error("AWS CUR object parse failed", "provider", domain.ProviderAWS, "ingestion_mode", "cur_s3", "bucket", c.cfg.CURBucket, "source_file_key", object.Key, "source_file_etag", object.ETag, "source_file_size", object.Size, "error", err)
			continue
		}
		result.FilesProcessed++
		c.logger.Info("AWS CUR S3 object streamed", "provider", domain.ProviderAWS, "ingestion_mode", "cur_s3", "bucket", c.cfg.CURBucket, "prefix", c.cfg.CURPrefix, "source_file_key", object.Key, "source_file_etag", object.ETag, "source_file_size", object.Size, "format", c.cfg.CURFormat, "records_parsed", parsed.RowsParsed, "records_skipped", parsed.RowsSkipped)
	}
	return result, errors.Join(errs...)
}

func (c *CURCollector) collectLocalFileStream(ctx context.Context, path string, handle providers.BatchHandler) (providers.CollectResult, error) {
	started := time.Now()
	file, err := os.Open(path)
	if err != nil {
		return providers.CollectResult{}, fmt.Errorf("open local AWS CUR file %q: %w", path, err)
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return providers.CollectResult{}, fmt.Errorf("stat local AWS CUR file %q: %w", path, err)
	}
	source := curSource{
		Bucket:       "local_file",
		Key:          path,
		Format:       c.cfg.CURFormat,
		LastModified: stat.ModTime().UTC(),
		Size:         stat.Size(),
	}
	metadata := domain.ProcessedReportFile{
		Provider:     domain.ProviderAWS,
		Bucket:       "local_file",
		ObjectName:   path,
		LastModified: stat.ModTime().UTC(),
	}
	var result providers.CollectResult
	parsed, err := c.streamCURSource(ctx, file, source, metadata, handle, &result)
	if err != nil {
		return providers.CollectResult{}, err
	}
	result.FilesProcessed = 1
	c.logger.Info("AWS CUR local file streamed", "provider", domain.ProviderAWS, "ingestion_mode", "cur_s3", "source", "local_file", "local_path", path, "format", c.cfg.CURFormat, "records_parsed", parsed.RowsParsed, "records_processed", result.RecordsProcessed, "records_skipped", parsed.RowsSkipped, "duration", time.Since(started).String())
	return result, nil
}

func (c *CURCollector) listCURObjects(ctx context.Context, client S3Client) ([]curObject, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -c.cfg.CURLookbackDays)
	var token *string
	var objects []curObject
	for {
		out, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(c.cfg.CURBucket),
			Prefix:            aws.String(c.cfg.CURPrefix),
			ContinuationToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("list AWS CUR S3 objects bucket=%s prefix=%s: %w", c.cfg.CURBucket, c.cfg.CURPrefix, err)
		}
		for _, object := range out.Contents {
			key := aws.ToString(object.Key)
			if !c.supportedCURKey(key) {
				continue
			}
			lastModified := object.LastModified
			if lastModified != nil && lastModified.UTC().Before(cutoff) {
				continue
			}
			objects = append(objects, curObject{
				Key:          key,
				ETag:         strings.Trim(aws.ToString(object.ETag), `"`),
				LastModified: timeValueUTC(lastModified),
				Size:         aws.ToInt64(object.Size),
			})
		}
		if out.NextContinuationToken == nil || strings.TrimSpace(*out.NextContinuationToken) == "" {
			break
		}
		token = out.NextContinuationToken
	}
	sort.Slice(objects, func(i, j int) bool {
		if objects[i].LastModified.Equal(objects[j].LastModified) {
			return objects[i].Key < objects[j].Key
		}
		return objects[i].LastModified.After(objects[j].LastModified)
	})
	return objects, nil
}

func (c *CURCollector) collectS3ObjectStream(ctx context.Context, client S3Client, object curObject, handle providers.BatchHandler, result *providers.CollectResult) (curParseResult, error) {
	out, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(c.cfg.CURBucket), Key: aws.String(object.Key)})
	if err != nil {
		return curParseResult{}, fmt.Errorf("download AWS CUR S3 object %s/%s: %w", c.cfg.CURBucket, object.Key, err)
	}
	defer out.Body.Close()
	source := curSource{
		Bucket:       c.cfg.CURBucket,
		Key:          object.Key,
		ETag:         object.ETag,
		Format:       c.cfg.CURFormat,
		LastModified: object.LastModified,
		Size:         object.Size,
	}
	metadata := domain.ProcessedReportFile{
		Provider:     domain.ProviderAWS,
		Bucket:       c.cfg.CURBucket,
		ObjectName:   object.Key,
		ETag:         object.ETag,
		LastModified: object.LastModified,
	}
	return c.streamCURSource(ctx, out.Body, source, metadata, handle, result)
}

func (c *CURCollector) streamCURSource(ctx context.Context, raw io.Reader, source curSource, metadata domain.ProcessedReportFile, handle providers.BatchHandler, result *providers.CollectResult) (curParseResult, error) {
	reader, closeReader, err := c.curReader(raw, source.Key)
	if err != nil {
		return curParseResult{}, err
	}
	defer closeReader()

	limits := c.cfg.IngestionLimits()
	buffer := make([]domain.RawBillingRecord, 0, limits.MaxMemoryBufferRecords)
	firstBatch := true
	accepted := 0
	flush := func() error {
		if len(buffer) == 0 {
			return nil
		}
		records := buffer
		if err := handle(ctx, providers.FileBatch{
			Metadata: metadata,
			Records:  records,
			First:    firstBatch,
		}); err != nil {
			return err
		}
		firstBatch = false
		result.BatchesInserted++
		buffer = make([]domain.RawBillingRecord, 0, limits.MaxMemoryBufferRecords)
		return nil
	}

	parsed, err := parseCURCSVStream(reader, source, func(record domain.RawBillingRecord) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		buffer = append(buffer, record)
		accepted++
		result.RecordsProcessed++
		if len(buffer) >= limits.MaxMemoryBufferRecords {
			return flush()
		}
		return nil
	})
	if err == nil {
		err = flush()
	}
	if err == nil {
		metadata.RecordCount = accepted
		metadata.ProcessedAt = time.Now().UTC()
		metadata.Status = "processed"
		err = handle(ctx, providers.FileBatch{Metadata: metadata, First: firstBatch, Final: true})
	}
	if err != nil {
		if !firstBatch {
			if cleanupErr := c.cleanupPartialIngest(source.Key); cleanupErr != nil {
				return parsed, errors.Join(err, cleanupErr)
			}
		}
		return parsed, err
	}
	return parsed, nil
}

func (c *CURCollector) cleanupPartialIngest(sourceObject string) error {
	if c.repo == nil || strings.TrimSpace(sourceObject) == "" {
		return nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := c.repo.DeleteCostRecordsForSource(cleanupCtx, domain.ProviderAWS, sourceObject); err != nil {
		c.logger.Error("failed to clean up partial AWS CUR records", "source_file_key", sourceObject, "error", err)
		return err
	}
	c.logger.Info("partial AWS CUR records cleaned up", "source_file_key", sourceObject)
	return nil
}

func (c *CURCollector) curReader(reader io.Reader, name string) (io.Reader, func(), error) {
	format := strings.TrimSpace(strings.ToLower(c.cfg.CURFormat))
	if format == "parquet" {
		return nil, func() {}, fmt.Errorf("AWS CUR format parquet is not supported yet; use csv or csv_gzip")
	}
	if format == "csv_gzip" || strings.HasSuffix(strings.ToLower(name), ".gz") {
		gz, err := gzip.NewReader(reader)
		if err != nil {
			return nil, func() {}, fmt.Errorf("open gzip AWS CUR file %q: %w", name, err)
		}
		return gz, func() { _ = gz.Close() }, nil
	}
	if format == "csv" || strings.HasSuffix(strings.ToLower(name), ".csv") {
		return reader, func() {}, nil
	}
	return nil, func() {}, fmt.Errorf("unsupported AWS CUR format %q", c.cfg.CURFormat)
}

func (c *CURCollector) supportedCURKey(key string) bool {
	lower := strings.ToLower(key)
	switch c.cfg.CURFormat {
	case "csv":
		return strings.HasSuffix(lower, ".csv")
	case "csv_gzip":
		return strings.HasSuffix(lower, ".csv.gz") || strings.HasSuffix(lower, ".gz")
	default:
		return strings.HasSuffix(lower, ".csv") || strings.HasSuffix(lower, ".csv.gz") || strings.HasSuffix(lower, ".gz")
	}
}

func timeValueUTC(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}
