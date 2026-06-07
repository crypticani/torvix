package intelligence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/crypticani/torvix/internal/domain"
	aiport "github.com/crypticani/torvix/internal/ports/ai"
	"github.com/crypticani/torvix/internal/waste"
)

type Config struct {
	Enabled            bool
	Provider           string
	Model              string
	Timeout            time.Duration
	MaxItemsPerRun     int
	QueueSize          int
	IncludeIdentifiers bool
}

type task struct {
	entityType string
	entityID   int64
	kind       string
	input      map[string]any
}

type Service struct {
	config  Config
	client  aiport.Client
	repo    aiport.Repository
	logger  *slog.Logger
	queue   chan task
	wg      sync.WaitGroup
	once    sync.Once
	ctx     context.Context
	cancel  context.CancelFunc
	queueMu sync.RWMutex
	closed  bool
}

func New(config Config, client aiport.Client, repo aiport.Repository, logger *slog.Logger) *Service {
	if config.Timeout <= 0 {
		config.Timeout = 20 * time.Second
	}
	if config.MaxItemsPerRun <= 0 {
		config.MaxItemsPerRun = 10
	}
	if config.QueueSize <= 0 {
		config.QueueSize = 100
	}
	if logger == nil {
		logger = slog.Default()
	}
	serviceCtx, cancel := context.WithCancel(context.Background())
	service := &Service{
		config: config,
		client: client,
		repo:   repo,
		logger: logger,
		queue:  make(chan task, config.QueueSize),
		ctx:    serviceCtx,
		cancel: cancel,
	}
	if service.Enabled() {
		service.wg.Add(1)
		go service.run()
	}
	return service
}

func (s *Service) Enabled() bool {
	return s != nil && s.config.Enabled && s.client != nil && s.repo != nil
}

func (s *Service) SubmitAnomalies(anomalies []domain.DashboardAnomaly) {
	if !s.Enabled() {
		return
	}
	for i, anomaly := range anomalies {
		if i >= s.config.MaxItemsPerRun {
			break
		}
		if anomaly.ID <= 0 {
			continue
		}
		s.submit(task{
			entityType: domain.AIEntityAnomaly,
			entityID:   anomaly.ID,
			kind:       "cost_anomaly",
			input:      s.anomalyInput(anomaly),
		})
	}
}

func (s *Service) SubmitWasteFindings(findings []waste.Finding) {
	if !s.Enabled() {
		return
	}
	for i, finding := range findings {
		if i >= s.config.MaxItemsPerRun {
			break
		}
		if finding.ID <= 0 {
			continue
		}
		s.submit(task{
			entityType: domain.AIEntityWaste,
			entityID:   finding.ID,
			kind:       "waste_finding",
			input:      s.wasteInput(finding),
		})
	}
}

func (s *Service) submit(value task) {
	s.queueMu.RLock()
	defer s.queueMu.RUnlock()
	if s.closed {
		return
	}
	select {
	case s.queue <- value:
	default:
		s.logger.Warn("AI enrichment queue full", "entity_type", value.entityType, "entity_id", value.entityID)
	}
}

func (s *Service) run() {
	defer s.wg.Done()
	for value := range s.queue {
		s.process(value)
	}
}

func (s *Service) process(value task) {
	inputHash, err := hashInput(value.input)
	if err != nil {
		s.logger.Error("hash AI enrichment input", "entity_type", value.entityType, "entity_id", value.entityID, "error", err)
		return
	}

	ctx, cancel := context.WithTimeout(s.ctx, s.config.Timeout)
	defer cancel()

	existing, found, err := s.repo.GetAIEnrichment(ctx, value.entityType, value.entityID)
	if err != nil {
		s.logger.Error("load AI enrichment", "entity_type", value.entityType, "entity_id", value.entityID, "error", err)
		return
	}
	if found &&
		existing.Status == domain.AIStatusCompleted &&
		existing.InputHash == inputHash &&
		existing.Provider == s.config.Provider &&
		existing.Model == s.config.Model {
		return
	}
	reusable, found, err := s.repo.GetAIEnrichmentByInput(ctx, value.entityType, inputHash, s.config.Provider, s.config.Model)
	if err != nil {
		s.logger.Error("load reusable AI enrichment", "entity_type", value.entityType, "entity_id", value.entityID, "error", err)
		return
	}
	if found && reusable.Status == domain.AIStatusCompleted {
		reusable.EntityID = value.entityID
		reusable.UpdatedAt = time.Now().UTC()
		if err := s.save(reusable); err != nil {
			s.logger.Error("save reused AI enrichment", "entity_type", value.entityType, "entity_id", value.entityID, "error", err)
			return
		}
		s.logger.Info("AI enrichment reused", "entity_type", value.entityType, "entity_id", value.entityID, "provider", s.config.Provider, "model", s.config.Model)
		return
	}

	result, generateErr := s.client.Generate(ctx, aiport.Request{Kind: value.kind, Context: value.input})
	now := time.Now().UTC()
	enrichment := domain.AIEnrichment{
		EntityType: value.entityType,
		EntityID:   value.entityID,
		InputHash:  inputHash,
		Provider:   s.config.Provider,
		Model:      s.config.Model,
		UpdatedAt:  now,
	}
	if generateErr != nil {
		enrichment.Status = domain.AIStatusFailed
		enrichment.Error = truncate(generateErr.Error(), 2000)
	} else {
		enrichment.Status = domain.AIStatusCompleted
		enrichment.Summary = result.Summary
		enrichment.LikelyCause = result.LikelyCause
		enrichment.BusinessImpact = result.BusinessImpact
		enrichment.RecommendedActions = result.RecommendedActions
		enrichment.Priority = result.Priority
		enrichment.Confidence = result.Confidence
		enrichment.GeneratedAt = &now
	}
	if err := s.save(enrichment); err != nil {
		s.logger.Error("save AI enrichment", "entity_type", value.entityType, "entity_id", value.entityID, "error", err)
		return
	}
	if generateErr != nil {
		s.logger.Warn("AI enrichment failed", "entity_type", value.entityType, "entity_id", value.entityID, "error", generateErr)
		return
	}
	s.logger.Info("AI enrichment completed", "entity_type", value.entityType, "entity_id", value.entityID, "provider", s.config.Provider, "model", s.config.Model)
}

func (s *Service) Close() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		s.cancel()
		if s.Enabled() {
			s.queueMu.Lock()
			s.closed = true
			close(s.queue)
			s.queueMu.Unlock()
			s.wg.Wait()
		}
	})
}

func (s *Service) save(enrichment domain.AIEnrichment) error {
	saveCtx, saveCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer saveCancel()
	return s.repo.UpsertAIEnrichment(saveCtx, enrichment)
}

func (s *Service) anomalyInput(anomaly domain.DashboardAnomaly) map[string]any {
	input := map[string]any{
		"provider":           anomaly.Provider,
		"service":            anomaly.Service,
		"category":           anomaly.Category,
		"region":             anomaly.Region,
		"currency":           anomaly.Currency,
		"observed_cost":      anomaly.ObservedCost,
		"expected_cost":      anomaly.ExpectedCost,
		"absolute_delta":     anomaly.AbsoluteDelta,
		"percentage_delta":   anomaly.PercentageDelta,
		"direction":          anomaly.Direction,
		"severity":           anomaly.Severity,
		"detection_method":   anomaly.DetectionMethod,
		"period_start":       anomaly.PeriodStart.UTC().Format(time.RFC3339),
		"deterministic_note": anomaly.Explanation,
	}
	if s.config.IncludeIdentifiers {
		input["account_id"] = anomaly.AccountID
		input["compartment_id"] = anomaly.CompartmentID
		input["compartment_name"] = anomaly.CompartmentName
	}
	return input
}

func (s *Service) wasteInput(finding waste.Finding) map[string]any {
	input := map[string]any{
		"provider":                 finding.Provider,
		"resource_type":            finding.ResourceType,
		"region":                   finding.Region,
		"service":                  finding.Service,
		"rule_id":                  finding.RuleID,
		"severity":                 finding.Severity,
		"deterministic_confidence": finding.Confidence,
		"currency":                 finding.Currency,
		"summary":                  finding.Summary,
		"recommendation":           finding.Recommendation,
	}
	if finding.EstimatedMonthlyWaste != nil {
		input["estimated_monthly_waste"] = *finding.EstimatedMonthlyWaste
	}
	if s.config.IncludeIdentifiers {
		input["resource_id"] = finding.ResourceID
		input["resource_name"] = finding.ResourceName
		input["scope_id"] = finding.ScopeID
		input["scope_name"] = finding.ScopeName
	}
	return input
}

func hashInput(input map[string]any) (string, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("marshal input: %w", err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func entityTypeKey(entityType string, entityID int64) string {
	return strings.TrimSpace(entityType) + ":" + strconv.FormatInt(entityID, 10)
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
