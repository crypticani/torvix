package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/crypticani/torvix/internal/domain"
	"github.com/crypticani/torvix/internal/waste"
	"github.com/crypticani/torvix/internal/waste/rules"
)

var ErrRunAlreadyActive = errors.New("waste detection already running")

type Service struct {
	cfg       waste.Config
	repo      waste.Repository
	providers map[domain.Provider]waste.InventoryProvider
	logger    *slog.Logger
	mu        sync.Mutex
	running   bool
}

func NewService(cfg waste.Config, repo waste.Repository, providers []waste.InventoryProvider, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	cfg = WithDefaults(cfg)
	byProvider := make(map[domain.Provider]waste.InventoryProvider, len(providers))
	for _, provider := range providers {
		if provider != nil {
			byProvider[provider.Provider()] = provider
		}
	}
	return &Service{cfg: cfg, repo: repo, providers: byProvider, logger: logger}
}

func (s *Service) Run(ctx context.Context) (waste.DetectionResult, error) {
	started := time.Now()
	if !s.cfg.Enabled {
		return waste.DetectionResult{Provider: s.cfg.Provider, Skipped: true, Message: "waste detection disabled"}, nil
	}
	if s.repo == nil {
		return waste.DetectionResult{Provider: s.cfg.Provider, Skipped: true, Message: "waste repository is not configured"}, nil
	}
	if !s.tryLock() {
		s.logger.Info("skipping waste detection because a run is already active", "provider", s.cfg.Provider)
		return waste.DetectionResult{Provider: s.cfg.Provider, Skipped: true, Message: ErrRunAlreadyActive.Error()}, ErrRunAlreadyActive
	}
	defer s.unlock()

	provider := s.cfg.Provider
	if provider == "" {
		provider = domain.ProviderOCI
	}
	s.logger.Info("waste detection started", "provider", provider)
	inventoryProvider := s.providers[provider]
	if inventoryProvider != nil {
		inventory, err := inventoryProvider.Sync(ctx)
		if err != nil {
			return waste.DetectionResult{Provider: provider, Duration: time.Since(started)}, fmt.Errorf("sync waste inventory: %w", err)
		}
		if inventory.Skipped {
			s.logger.Info("waste inventory sync skipped", "provider", provider, "message", inventory.Message)
		}
	}
	if provider == domain.ProviderAWS {
		message := "AWS waste detection is not implemented in Phase 1. Cost Explorer support remains available."
		s.logger.Info(message, "provider", provider)
		return waste.DetectionResult{Provider: provider, Skipped: true, Message: message, Duration: time.Since(started)}, nil
	}
	if provider != domain.ProviderOCI {
		message := fmt.Sprintf("waste detection provider %q is not implemented", provider)
		s.logger.Info(message, "provider", provider)
		return waste.DetectionResult{Provider: provider, Skipped: true, Message: message, Duration: time.Since(started)}, nil
	}

	resources, err := s.repo.ListCloudResources(ctx, provider)
	if err != nil {
		return waste.DetectionResult{Provider: provider, Duration: time.Since(started)}, fmt.Errorf("list cloud resources: %w", err)
	}
	relationships, err := s.repo.ListCloudRelationships(ctx, provider)
	if err != nil {
		return waste.DetectionResult{Provider: provider, Duration: time.Since(started)}, fmt.Errorf("list cloud relationships: %w", err)
	}
	costs := make(map[string]waste.CostSignal, len(resources))
	now := time.Now().UTC()
	for _, resource := range resources {
		signal, err := s.repo.GetResourceCostSignal(ctx, provider, resource.ResourceID, now)
		if err != nil {
			s.logger.Warn("failed to correlate resource cost", "provider", provider, "resource_id", resource.ResourceID, "error", err)
			continue
		}
		costs[resource.ResourceID] = signal
	}
	evaluated := rules.EvaluateOCI(rules.EvaluationInput{
		Config:        s.cfg,
		Now:           now,
		Resources:     resources,
		Relationships: relationships,
		Costs:         costs,
	})
	var created, updated int
	seen := make(map[string]struct{}, len(evaluated.Findings))
	for _, finding := range evaluated.Findings {
		outcome, err := s.repo.UpsertWasteFinding(ctx, finding)
		if err != nil {
			return waste.DetectionResult{Provider: provider, Duration: time.Since(started)}, fmt.Errorf("upsert waste finding: %w", err)
		}
		switch outcome {
		case "created":
			created++
		default:
			updated++
		}
		seen[finding.ResourceID+"|"+finding.RuleID] = struct{}{}
	}
	ruleIDs := make([]string, 0, len(rules.RuleCatalog()))
	for _, rule := range rules.RuleCatalog() {
		ruleIDs = append(ruleIDs, rule.RuleID)
	}
	resolved, err := s.repo.ResolveMissingWasteFindings(ctx, provider, ruleIDs, seen)
	if err != nil {
		return waste.DetectionResult{Provider: provider, Duration: time.Since(started)}, fmt.Errorf("resolve missing waste findings: %w", err)
	}
	result := waste.DetectionResult{
		Provider:         provider,
		ResourcesScanned: len(resources),
		FindingsCreated:  created,
		FindingsUpdated:  updated,
		FindingsResolved: resolved,
		ResourcesSkipped: evaluated.Skipped,
		Duration:         time.Since(started),
	}
	s.logger.Info("waste detection completed",
		"provider", provider,
		"resources_scanned", result.ResourcesScanned,
		"findings_created", created,
		"findings_updated", updated,
		"findings_resolved", resolved,
		"resources_skipped", evaluated.Skipped,
		"duration", result.Duration.String(),
	)
	return result, nil
}

func (s *Service) Rules() []waste.RuleInfo {
	return rules.RuleCatalog()
}

func (s *Service) ListFindings(ctx context.Context, filters waste.FindingFilters) ([]waste.Finding, error) {
	if s.repo == nil {
		return nil, nil
	}
	return s.repo.ListWasteFindings(ctx, filters)
}

func (s *Service) GetFinding(ctx context.Context, id int64) (waste.Finding, error) {
	if s.repo == nil {
		return waste.Finding{}, fmt.Errorf("waste repository is not configured")
	}
	return s.repo.GetWasteFinding(ctx, id)
}

func (s *Service) UpdateFindingStatus(ctx context.Context, id int64, status string) (waste.Finding, error) {
	if !validStatus(status) {
		return waste.Finding{}, fmt.Errorf("invalid waste finding status: %s", status)
	}
	if s.repo == nil {
		return waste.Finding{}, fmt.Errorf("waste repository is not configured")
	}
	return s.repo.UpdateWasteFindingStatus(ctx, id, status)
}

func (s *Service) Summary(ctx context.Context, filters waste.FindingFilters) (waste.Summary, error) {
	filters.Status = waste.StatusOpen
	if filters.Limit <= 0 || filters.Limit > 10 {
		filters.Limit = 10
	}
	top, err := s.ListFindings(ctx, filters)
	if err != nil {
		return waste.Summary{}, err
	}
	allFilters := filters
	allFilters.Limit = 10000
	allFilters.Offset = 0
	findings, err := s.ListFindings(ctx, allFilters)
	if err != nil {
		return waste.Summary{}, err
	}
	summary := waste.Summary{
		FindingsBySeverity: make(map[string]int64),
		FindingsByRule:     make(map[string]int64),
		FindingsByRegion:   make(map[string]int64),
		FindingsByScope:    make(map[string]int64),
		FindingsByService:  make(map[string]int64),
		TopFindings:        top,
	}
	for _, finding := range findings {
		summary.TotalOpenFindings++
		if finding.EstimatedMonthlyWaste != nil {
			summary.EstimatedMonthlyWaste += *finding.EstimatedMonthlyWaste
		}
		increment(summary.FindingsBySeverity, finding.Severity)
		increment(summary.FindingsByRule, finding.RuleID)
		increment(summary.FindingsByRegion, finding.Region)
		increment(summary.FindingsByScope, finding.ScopeName)
		increment(summary.FindingsByService, finding.Service)
	}
	return summary, nil
}

func (s *Service) tryLock() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return false
	}
	s.running = true
	return true
}

func (s *Service) unlock() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
}

func increment(values map[string]int64, key string) {
	if key == "" {
		key = "unknown"
	}
	values[key]++
}

func validStatus(status string) bool {
	switch status {
	case waste.StatusOpen, waste.StatusAccepted, waste.StatusIgnored, waste.StatusFalsePositive, waste.StatusFixed, waste.StatusResolved:
		return true
	default:
		return false
	}
}

func WithDefaults(c waste.Config) waste.Config {
	if c.Provider == "" {
		c.Provider = domain.ProviderOCI
	}
	if c.ScanIntervalHours <= 0 {
		c.ScanIntervalHours = 24
	}
	if c.MinResourceAgeDays <= 0 {
		c.MinResourceAgeDays = 7
	}
	if c.StoppedInstanceMinDays <= 0 {
		c.StoppedInstanceMinDays = 3
	}
	if c.OldBackupDays <= 0 {
		c.OldBackupDays = 30
	}
	if c.HighMonthlyThreshold <= 0 {
		c.HighMonthlyThreshold = 50
	}
	if c.Currency == "" {
		c.Currency = "USD"
	}
	if len(c.ExclusionTagKeys) == 0 {
		c.ExclusionTagKeys = []string{"torvix:ignore", "torvix:waste-ignore", "finops:ignore", "keep", "retain", "do-not-delete"}
	}
	return c
}
