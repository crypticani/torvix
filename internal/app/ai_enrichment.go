package app

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	openaiadapter "github.com/crypticani/torvix/internal/adapters/ai/openai"
	"github.com/crypticani/torvix/internal/config"
	"github.com/crypticani/torvix/internal/core/collect"
	"github.com/crypticani/torvix/internal/core/intelligence"
	"github.com/crypticani/torvix/internal/domain"
	aiport "github.com/crypticani/torvix/internal/ports/ai"
	"github.com/crypticani/torvix/internal/waste"
)

type anomalyRepository interface {
	DashboardAnomalies(ctx context.Context, from, to time.Time, severity string) ([]domain.DashboardAnomaly, error)
}

type enrichingCollector struct {
	next         ingestionRunner
	repo         anomalyRepository
	intelligence *intelligence.Service
	lookbackDays int
	logger       *slog.Logger
}

func (c *enrichingCollector) Run(ctx context.Context, since time.Time) ([]collect.ProviderResult, error) {
	results, err := c.next.Run(ctx, since)
	if err != nil || c.intelligence == nil || !c.intelligence.Enabled() {
		return results, err
	}
	lookbackDays := c.lookbackDays
	if lookbackDays <= 0 {
		lookbackDays = 30
	}
	now := time.Now().UTC()
	anomalies, queryErr := c.repo.DashboardAnomalies(ctx, now.AddDate(0, 0, -lookbackDays), now.Add(24*time.Hour), "")
	if queryErr == nil {
		c.intelligence.SubmitAnomalies(anomalies)
	} else if c.logger != nil {
		c.logger.Warn("AI anomaly enrichment skipped", "error", queryErr)
	}
	return results, err
}

type enrichingWasteDetector struct {
	next         waste.Detector
	intelligence *intelligence.Service
	maxItems     int
	logger       *slog.Logger
}

func (d *enrichingWasteDetector) Run(ctx context.Context) (waste.DetectionResult, error) {
	result, err := d.next.Run(ctx)
	if err != nil || d.intelligence == nil || !d.intelligence.Enabled() {
		return result, err
	}
	findings, listErr := d.next.ListFindings(ctx, waste.FindingFilters{
		Status: waste.StatusOpen,
		Limit:  d.maxItems,
	})
	if listErr == nil {
		d.intelligence.SubmitWasteFindings(findings)
	} else if d.logger != nil {
		d.logger.Warn("AI waste enrichment skipped", "error", listErr)
	}
	return result, err
}

func (d *enrichingWasteDetector) Rules() []waste.RuleInfo {
	return d.next.Rules()
}

func (d *enrichingWasteDetector) ListFindings(ctx context.Context, filters waste.FindingFilters) ([]waste.Finding, error) {
	return d.next.ListFindings(ctx, filters)
}

func (d *enrichingWasteDetector) GetFinding(ctx context.Context, id int64) (waste.Finding, error) {
	return d.next.GetFinding(ctx, id)
}

func (d *enrichingWasteDetector) UpdateFindingStatus(ctx context.Context, id int64, status string) (waste.Finding, error) {
	return d.next.UpdateFindingStatus(ctx, id, status)
}

func (d *enrichingWasteDetector) Summary(ctx context.Context, filters waste.FindingFilters) (waste.Summary, error) {
	return d.next.Summary(ctx, filters)
}

func newIntelligenceService(cfg config.AI, repo aiport.Repository, logger *slog.Logger) *intelligence.Service {
	serviceConfig := intelligence.Config{
		Enabled:            false,
		Provider:           cfg.Provider,
		Model:              cfg.Model,
		MaxItemsPerRun:     cfg.MaxItemsPerRun,
		QueueSize:          cfg.QueueSize,
		IncludeIdentifiers: cfg.IncludeIdentifiers,
	}
	timeout, err := time.ParseDuration(cfg.Timeout)
	if err != nil || timeout <= 0 {
		timeout = 20 * time.Second
		if logger != nil && cfg.Enabled {
			logger.Warn("invalid AI timeout; using default", "configured", cfg.Timeout, "default", timeout.String())
		}
	}
	serviceConfig.Timeout = timeout

	if !cfg.Enabled {
		return intelligence.New(serviceConfig, nil, repo, logger)
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		if logger != nil {
			logger.Warn("AI enrichment disabled", "reason", "OPENAI_API_KEY is not configured")
		}
		return intelligence.New(serviceConfig, nil, repo, logger)
	}
	if cfg.Provider != "openai" {
		if logger != nil {
			logger.Warn("AI enrichment disabled", "reason", "unsupported provider", "provider", cfg.Provider)
		}
		return intelligence.New(serviceConfig, nil, repo, logger)
	}

	serviceConfig.Enabled = true
	client := openaiadapter.New(openaiadapter.Config{
		APIKey:  cfg.APIKey,
		BaseURL: cfg.BaseURL,
		Model:   cfg.Model,
	}, &http.Client{Timeout: timeout})
	if logger != nil {
		logger.Info("AI enrichment enabled", "provider", cfg.Provider, "model", cfg.Model, "max_items_per_run", cfg.MaxItemsPerRun, "include_identifiers", cfg.IncludeIdentifiers)
	}
	return intelligence.New(serviceConfig, client, repo, logger)
}
