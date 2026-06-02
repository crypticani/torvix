package aws

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"

	"github.com/crypticani/torvix/internal/config"
	"github.com/crypticani/torvix/internal/domain"
	"github.com/crypticani/torvix/internal/ports/providers"
)

type Collector struct {
	cfg    config.AWSProvider
	logger *slog.Logger
	client CostExplorerClient
}

func New(ctx context.Context, cfg config.AWSProvider, logger *slog.Logger) (providers.Collector, error) {
	cfg = cfg.WithDefaults()
	switch cfg.IngestionMode {
	case "cur_s3":
		return NewCURCollector(cfg, logger, nil), nil
	case "cost_explorer":
	default:
		return nil, fmt.Errorf("unknown AWS ingestion mode %q; expected cur_s3 or cost_explorer", cfg.IngestionMode)
	}
	client, err := NewCostExplorerClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return NewWithClient(cfg, logger, client), nil
}

func NewWithClient(cfg config.AWSProvider, logger *slog.Logger, client CostExplorerClient) *Collector {
	if logger == nil {
		logger = slog.Default()
	}
	return &Collector{cfg: cfg.WithDefaults(), logger: logger, client: client}
}

func (c *Collector) Name() string { return string(domain.ProviderAWS) }

func (c *Collector) Collect(ctx context.Context, _ time.Time) (providers.CollectResult, error) {
	if !c.cfg.Enabled {
		c.logger.Info("AWS collector skipped because provider is disabled", "provider", domain.ProviderAWS)
		return providers.CollectResult{}, nil
	}
	started := time.Now()
	start, end := c.collectionWindow(time.Now().UTC())
	queries := defaultQueries()
	allRecords := make([]domain.RawBillingRecord, 0)
	var errs []error
	var result providers.CollectResult
	estimatedCount := 0
	for _, query := range queries {
		queryStarted := time.Now()
		qr, err := c.collectQuery(ctx, query, start, end)
		if err != nil {
			result.Failures++
			if query.Optional {
				c.logger.Warn("AWS optional Cost Explorer query failed; continuing", "provider", domain.ProviderAWS, "query_type", query.Type, "start_date", start.Format(time.DateOnly), "end_date", end.Format(time.DateOnly), "error", err)
				continue
			}
			errs = append(errs, err)
			c.logger.Error("AWS Cost Explorer query failed", "provider", domain.ProviderAWS, "query_type", query.Type, "start_date", start.Format(time.DateOnly), "end_date", end.Format(time.DateOnly), "error", err)
			continue
		}
		allRecords = append(allRecords, qr.Records...)
		estimatedCount += qr.EstimatedCount
		c.logger.Info("AWS Cost Explorer query completed", "provider", domain.ProviderAWS, "query_type", query.Type, "start_date", start.Format(time.DateOnly), "end_date", end.Format(time.DateOnly), "records_inserted", len(qr.Records), "records_updated", 0, "records_skipped", 0, "estimated_count", qr.EstimatedCount, "duration", time.Since(queryStarted).String())
	}
	if len(allRecords) > 0 {
		for i := range allRecords {
			allRecords[i].SourceObject = fmt.Sprintf("aws-cost-explorer:%s:%s", start.Format(time.DateOnly), end.Format(time.DateOnly))
		}
		result.Batches = []providers.FileBatch{{
			Metadata: domain.ProcessedReportFile{
				Provider:     domain.ProviderAWS,
				Bucket:       "cost-explorer",
				ObjectName:   fmt.Sprintf("aws-cost-explorer:%s:%s", start.Format(time.DateOnly), end.Format(time.DateOnly)),
				ETag:         c.cfg.CostMetric,
				LastModified: time.Now().UTC(),
			},
			Records: allRecords,
		}}
		result.FilesProcessed = 1
	}
	result.RecordsProcessed = len(allRecords)
	c.logger.Info("AWS collection completed", "provider", domain.ProviderAWS, "start_date", start.Format(time.DateOnly), "end_date", end.Format(time.DateOnly), "records_inserted", len(allRecords), "records_updated", 0, "records_skipped", 0, "duration", time.Since(started).String(), "estimated_count", estimatedCount, "error", errors.Join(errs...))
	return result, errors.Join(errs...)
}

func (c *Collector) collectQuery(ctx context.Context, query costQuery, start, end time.Time) (queryResult, error) {
	var out queryResult
	var nextToken *string
	for {
		input := c.costAndUsageInput(query, start, end, nextToken)
		resp, err := c.client.GetCostAndUsage(ctx, input)
		if err != nil {
			return out, classifyCostExplorerError(err)
		}
		records, estimated, err := mapCostAndUsageOutput(query.Type, c.cfg.CostMetric, resp)
		if err != nil {
			return out, err
		}
		out.Records = append(out.Records, records...)
		out.EstimatedCount += estimated
		if resp == nil || resp.NextPageToken == nil || strings.TrimSpace(*resp.NextPageToken) == "" {
			return out, nil
		}
		nextToken = resp.NextPageToken
	}
}

func (c *Collector) costAndUsageInput(query costQuery, start, end time.Time, nextToken *string) *costexplorer.GetCostAndUsageInput {
	input := &costexplorer.GetCostAndUsageInput{
		Granularity: types.GranularityDaily,
		Metrics:     []string{c.cfg.CostMetric},
		TimePeriod: &types.DateInterval{
			Start: stringPtr(start.Format(time.DateOnly)),
			End:   stringPtr(end.Format(time.DateOnly)),
		},
		NextPageToken: nextToken,
	}
	for _, dimension := range query.GroupBy {
		key := string(dimension)
		input.GroupBy = append(input.GroupBy, types.GroupDefinition{
			Type: types.GroupDefinitionTypeDimension,
			Key:  &key,
		})
	}
	return input
}

func (c *Collector) collectionWindow(now time.Time) (time.Time, time.Time) {
	today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	return today.AddDate(0, 0, -c.cfg.LookbackDays), today
}

func defaultQueries() []costQuery {
	return []costQuery{
		{Type: queryTotal},
		{Type: queryService, GroupBy: []types.Dimension{types.DimensionService}},
		{Type: queryLinkedAccount, GroupBy: []types.Dimension{types.DimensionLinkedAccount}},
		{Type: queryRegion, GroupBy: []types.Dimension{types.DimensionRegion}, Optional: true},
		{Type: queryLinkedAccountService, GroupBy: []types.Dimension{types.DimensionLinkedAccount, types.DimensionService}},
		{Type: queryRegionService, GroupBy: []types.Dimension{types.DimensionRegion, types.DimensionService}, Optional: true},
	}
}

func classifyCostExplorerError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("AWS Cost Explorer request failed: %w", err)
}

func stringPtr(value string) *string {
	return &value
}
