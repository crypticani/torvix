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

type CURCollector struct {
	cfg    config.AWSProvider
	logger *slog.Logger
	client S3Client
}

type curObject struct {
	Key          string
	ETag         string
	LastModified time.Time
	Size         int64
}

func NewCURCollector(cfg config.AWSProvider, logger *slog.Logger, client S3Client) *CURCollector {
	if logger == nil {
		logger = slog.Default()
	}
	return &CURCollector{cfg: cfg.WithDefaults(), logger: logger, client: client}
}

func (c *CURCollector) Name() string { return string(domain.ProviderAWS) }

func (c *CURCollector) Collect(ctx context.Context, _ time.Time) (providers.CollectResult, error) {
	if !c.cfg.Enabled {
		c.logger.Info("AWS CUR collector skipped because provider is disabled", "provider", domain.ProviderAWS, "ingestion_mode", "cur_s3")
		return providers.CollectResult{}, nil
	}
	if strings.TrimSpace(c.cfg.CURLocalPath) != "" {
		return c.collectLocalFile(ctx, c.cfg.CURLocalPath)
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
	var result providers.CollectResult
	var errs []error
	for _, object := range objects {
		batch, parsed, err := c.collectS3Object(ctx, client, object)
		if err != nil {
			result.Failures++
			errs = append(errs, err)
			c.logger.Error("AWS CUR object parse failed", "provider", domain.ProviderAWS, "ingestion_mode", "cur_s3", "bucket", c.cfg.CURBucket, "source_file_key", object.Key, "source_file_etag", object.ETag, "source_file_size", object.Size, "error", err)
			continue
		}
		result.FilesProcessed++
		result.RecordsProcessed += parsed.RowsParsed
		if len(batch.Records) > 0 {
			result.Batches = append(result.Batches, batch)
		}
	}
	return result, errors.Join(errs...)
}

func (c *CURCollector) collectLocalFile(ctx context.Context, path string) (providers.CollectResult, error) {
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
	reader, closeReader, err := c.curReader(file, path)
	if err != nil {
		return providers.CollectResult{}, err
	}
	defer closeReader()
	source := curSource{
		Bucket:       "local_file",
		Key:          path,
		Format:       c.cfg.CURFormat,
		LastModified: stat.ModTime().UTC(),
		Size:         stat.Size(),
	}
	parsed, err := parseCURCSV(reader, source)
	if err != nil {
		return providers.CollectResult{}, err
	}
	batch := providers.FileBatch{
		Metadata: domain.ProcessedReportFile{
			Provider:     domain.ProviderAWS,
			Bucket:       "local_file",
			ObjectName:   path,
			ETag:         "",
			LastModified: stat.ModTime().UTC(),
			RecordCount:  len(parsed.Records),
		},
		Records: parsed.Records,
	}
	c.logger.Info("AWS CUR local file collected", "provider", domain.ProviderAWS, "ingestion_mode", "cur_s3", "source", "local_file", "local_path", path, "format", c.cfg.CURFormat, "records_processed", parsed.RowsParsed, "records_inserted", len(parsed.Records), "records_skipped", parsed.RowsSkipped, "duration", time.Since(started).String())
	return providers.CollectResult{FilesProcessed: 1, RecordsProcessed: parsed.RowsParsed, Batches: []providers.FileBatch{batch}}, nil
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

func (c *CURCollector) collectS3Object(ctx context.Context, client S3Client, object curObject) (providers.FileBatch, curParseResult, error) {
	started := time.Now()
	out, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(c.cfg.CURBucket), Key: aws.String(object.Key)})
	if err != nil {
		return providers.FileBatch{}, curParseResult{}, fmt.Errorf("download AWS CUR S3 object %s/%s: %w", c.cfg.CURBucket, object.Key, err)
	}
	defer out.Body.Close()
	reader, closeReader, err := c.curReader(out.Body, object.Key)
	if err != nil {
		return providers.FileBatch{}, curParseResult{}, err
	}
	defer closeReader()
	source := curSource{
		Bucket:       c.cfg.CURBucket,
		Key:          object.Key,
		ETag:         object.ETag,
		Format:       c.cfg.CURFormat,
		LastModified: object.LastModified,
		Size:         object.Size,
	}
	parsed, err := parseCURCSV(reader, source)
	if err != nil {
		return providers.FileBatch{}, parsed, err
	}
	c.logger.Info("AWS CUR S3 object collected", "provider", domain.ProviderAWS, "ingestion_mode", "cur_s3", "bucket", c.cfg.CURBucket, "prefix", c.cfg.CURPrefix, "source_file_key", object.Key, "source_file_etag", object.ETag, "source_file_size", object.Size, "format", c.cfg.CURFormat, "records_processed", parsed.RowsParsed, "records_inserted", len(parsed.Records), "records_skipped", parsed.RowsSkipped, "duration", time.Since(started).String())
	return providers.FileBatch{
		Metadata: domain.ProcessedReportFile{
			Provider:     domain.ProviderAWS,
			Bucket:       c.cfg.CURBucket,
			ObjectName:   object.Key,
			ETag:         object.ETag,
			LastModified: object.LastModified,
			RecordCount:  len(parsed.Records),
		},
		Records: parsed.Records,
	}, parsed, nil
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
