package intelligence

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/crypticani/torvix/internal/domain"
	aiport "github.com/crypticani/torvix/internal/ports/ai"
	"github.com/crypticani/torvix/internal/waste"
)

type fakeClient struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (f *fakeClient) Generate(context.Context, aiport.Request) (aiport.Result, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.err != nil {
		return aiport.Result{}, f.err
	}
	return aiport.Result{
		Summary:            "Review this finding.",
		LikelyCause:        "Usage changed.",
		BusinessImpact:     "Potential avoidable spend.",
		RecommendedActions: []string{"Confirm ownership"},
		Priority:           "medium",
		Confidence:         0.7,
	}, nil
}

func (f *fakeClient) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeRepository struct {
	mu          sync.Mutex
	enrichments map[string]domain.AIEnrichment
	saved       chan domain.AIEnrichment
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		enrichments: map[string]domain.AIEnrichment{},
		saved:       make(chan domain.AIEnrichment, 10),
	}
}

func (f *fakeRepository) GetAIEnrichment(_ context.Context, entityType string, entityID int64) (domain.AIEnrichment, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	value, ok := f.enrichments[entityTypeKey(entityType, entityID)]
	return value, ok, nil
}

func (f *fakeRepository) GetAIEnrichmentByInput(_ context.Context, entityType, inputHash, provider, model string) (domain.AIEnrichment, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, value := range f.enrichments {
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

func (f *fakeRepository) UpsertAIEnrichment(_ context.Context, value domain.AIEnrichment) error {
	f.mu.Lock()
	f.enrichments[entityTypeKey(value.EntityType, value.EntityID)] = value
	f.mu.Unlock()
	f.saved <- value
	return nil
}

func TestServiceEnrichesAnomalyAndSkipsUnchangedInput(t *testing.T) {
	client := &fakeClient{}
	repo := newFakeRepository()
	service := New(Config{
		Enabled:        true,
		Provider:       "openai",
		Model:          "gpt-5.4-mini",
		Timeout:        time.Second,
		MaxItemsPerRun: 10,
		QueueSize:      10,
	}, client, repo, slog.Default())
	defer service.Close()

	anomaly := domain.DashboardAnomaly{
		ID:              42,
		Provider:        domain.ProviderOCI,
		Service:         "Compute",
		Region:          "ap-mumbai-1",
		ObservedCost:    150,
		ExpectedCost:    100,
		PercentageDelta: 50,
		Direction:       "increase",
		Severity:        "high",
	}
	service.SubmitAnomalies([]domain.DashboardAnomaly{anomaly})
	first := waitForEnrichment(t, repo.saved)
	if first.Status != domain.AIStatusCompleted || first.EntityType != domain.AIEntityAnomaly {
		t.Fatalf("unexpected enrichment: %+v", first)
	}

	service.SubmitAnomalies([]domain.DashboardAnomaly{anomaly})
	select {
	case duplicate := <-repo.saved:
		t.Fatalf("unchanged input should not be enriched again: %+v", duplicate)
	case <-time.After(100 * time.Millisecond):
	}
	if client.callCount() != 1 {
		t.Fatalf("expected one client call, got %d", client.callCount())
	}
}

func TestServiceProviderFailureIsPersistedWithoutBlockingSubmission(t *testing.T) {
	client := &fakeClient{err: errors.New("provider unavailable")}
	repo := newFakeRepository()
	service := New(Config{
		Enabled:        true,
		Provider:       "openai",
		Model:          "gpt-5.4-mini",
		Timeout:        time.Second,
		MaxItemsPerRun: 1,
		QueueSize:      1,
	}, client, repo, slog.Default())
	defer service.Close()

	started := time.Now()
	service.SubmitWasteFindings([]waste.Finding{{ID: 7, Provider: domain.ProviderOCI, RuleID: "oci.detached_volume", Service: "Block Storage"}})
	if time.Since(started) > 50*time.Millisecond {
		t.Fatal("submission must not block on the provider")
	}
	got := waitForEnrichment(t, repo.saved)
	if got.Status != domain.AIStatusFailed || got.Error == "" {
		t.Fatalf("expected persisted failure, got %+v", got)
	}
}

func TestServiceReusesCompletedEnrichmentForRefreshedAnomalyID(t *testing.T) {
	client := &fakeClient{}
	repo := newFakeRepository()
	service := New(Config{
		Enabled:        true,
		Provider:       "openai",
		Model:          "gpt-5.4-mini",
		Timeout:        time.Second,
		MaxItemsPerRun: 10,
		QueueSize:      10,
	}, client, repo, slog.Default())
	defer service.Close()

	anomaly := domain.DashboardAnomaly{
		ID:              41,
		Provider:        domain.ProviderAWS,
		Service:         "EC2",
		Region:          "us-east-1",
		ObservedCost:    150,
		ExpectedCost:    100,
		PercentageDelta: 50,
		Direction:       "increase",
		Severity:        "high",
	}
	service.SubmitAnomalies([]domain.DashboardAnomaly{anomaly})
	_ = waitForEnrichment(t, repo.saved)
	if client.callCount() != 1 {
		t.Fatalf("expected initial provider call, got %d", client.callCount())
	}

	anomaly.ID = 42
	service.SubmitAnomalies([]domain.DashboardAnomaly{anomaly})
	reused := waitForEnrichment(t, repo.saved)
	if reused.EntityID != 42 || reused.Status != domain.AIStatusCompleted {
		t.Fatalf("expected enrichment copied to refreshed anomaly ID, got %+v", reused)
	}
	if client.callCount() != 1 {
		t.Fatalf("expected no additional provider call, got %d", client.callCount())
	}
}

func waitForEnrichment(t *testing.T, values <-chan domain.AIEnrichment) domain.AIEnrichment {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for enrichment")
		return domain.AIEnrichment{}
	}
}
