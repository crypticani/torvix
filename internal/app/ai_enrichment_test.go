package app

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/crypticani/torvix/internal/core/collect"
	"github.com/crypticani/torvix/internal/core/intelligence"
	"github.com/crypticani/torvix/internal/domain"
	aiport "github.com/crypticani/torvix/internal/ports/ai"
	"github.com/crypticani/torvix/internal/waste"
)

type appAIClient struct{}

func (appAIClient) Generate(context.Context, aiport.Request) (aiport.Result, error) {
	return aiport.Result{
		Summary:            "summary",
		LikelyCause:        "cause",
		BusinessImpact:     "impact",
		RecommendedActions: []string{"review"},
		Priority:           "medium",
		Confidence:         0.7,
	}, nil
}

type appAIRepository struct {
	mu     sync.Mutex
	values map[string]domain.AIEnrichment
	saved  chan domain.AIEnrichment
}

func newAppAIRepository() *appAIRepository {
	return &appAIRepository{values: map[string]domain.AIEnrichment{}, saved: make(chan domain.AIEnrichment, 2)}
}

func (r *appAIRepository) GetAIEnrichment(_ context.Context, entityType string, entityID int64) (domain.AIEnrichment, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.values[entityType]
	return value, ok, nil
}

func (r *appAIRepository) GetAIEnrichmentByInput(_ context.Context, entityType, inputHash, provider, model string) (domain.AIEnrichment, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, value := range r.values {
		if value.EntityType == entityType &&
			value.InputHash == inputHash &&
			value.Provider == provider &&
			value.Model == model &&
			value.Status == domain.AIStatusCompleted {
			return value, true, nil
		}
	}
	return domain.AIEnrichment{}, false, nil
}

func (r *appAIRepository) UpsertAIEnrichment(_ context.Context, value domain.AIEnrichment) error {
	r.mu.Lock()
	r.values[value.EntityType] = value
	r.mu.Unlock()
	r.saved <- value
	return nil
}

type appIngestionRunner struct{}

func (appIngestionRunner) Run(context.Context, time.Time) ([]collect.ProviderResult, error) {
	return []collect.ProviderResult{{Provider: "aws", RecordsInserted: 1}}, nil
}

type appAnomalyRepository struct{}

func (appAnomalyRepository) DashboardAnomalies(context.Context, time.Time, time.Time, string) ([]domain.DashboardAnomaly, error) {
	return []domain.DashboardAnomaly{{ID: 11, Provider: domain.ProviderAWS, Service: "EC2", Severity: "high"}}, nil
}

func TestEnrichingCollectorSubmitsAnomaliesAfterSuccessfulRun(t *testing.T) {
	repo := newAppAIRepository()
	service := intelligence.New(intelligence.Config{
		Enabled:        true,
		Provider:       "openai",
		Model:          "gpt-5.4-mini",
		Timeout:        time.Second,
		MaxItemsPerRun: 10,
		QueueSize:      10,
	}, appAIClient{}, repo, slog.Default())
	defer service.Close()

	runner := &enrichingCollector{
		next:         appIngestionRunner{},
		repo:         appAnomalyRepository{},
		intelligence: service,
		lookbackDays: 30,
	}
	if _, err := runner.Run(context.Background(), time.Time{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := waitForAppAIEnrichment(t, repo.saved); got.EntityType != domain.AIEntityAnomaly || got.EntityID != 11 {
		t.Fatalf("unexpected enrichment: %+v", got)
	}
}

type appWasteDetector struct{}

func (appWasteDetector) Run(context.Context) (waste.DetectionResult, error) {
	return waste.DetectionResult{FindingsCreated: 1}, nil
}

func (appWasteDetector) Rules() []waste.RuleInfo { return nil }

func (appWasteDetector) ListFindings(context.Context, waste.FindingFilters) ([]waste.Finding, error) {
	return []waste.Finding{{ID: 12, Provider: domain.ProviderOCI, Service: "Block Storage", RuleID: waste.RuleOCIDetachedBlockVolume}}, nil
}

func (appWasteDetector) GetFinding(context.Context, int64) (waste.Finding, error) {
	return waste.Finding{}, nil
}

func (appWasteDetector) UpdateFindingStatus(context.Context, int64, string) (waste.Finding, error) {
	return waste.Finding{}, nil
}

func (appWasteDetector) Summary(context.Context, waste.FindingFilters) (waste.Summary, error) {
	return waste.Summary{}, nil
}

func TestEnrichingWasteDetectorSubmitsOpenFindingsAfterSuccessfulRun(t *testing.T) {
	repo := newAppAIRepository()
	service := intelligence.New(intelligence.Config{
		Enabled:        true,
		Provider:       "openai",
		Model:          "gpt-5.4-mini",
		Timeout:        time.Second,
		MaxItemsPerRun: 10,
		QueueSize:      10,
	}, appAIClient{}, repo, slog.Default())
	defer service.Close()

	detector := &enrichingWasteDetector{next: appWasteDetector{}, intelligence: service, maxItems: 10}
	if _, err := detector.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := waitForAppAIEnrichment(t, repo.saved); got.EntityType != domain.AIEntityWaste || got.EntityID != 12 {
		t.Fatalf("unexpected enrichment: %+v", got)
	}
}

func waitForAppAIEnrichment(t *testing.T, values <-chan domain.AIEnrichment) domain.AIEnrichment {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for AI enrichment")
		return domain.AIEnrichment{}
	}
}
