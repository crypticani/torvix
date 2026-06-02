package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/crypticani/torvix/internal/domain"
	"github.com/crypticani/torvix/internal/waste"
)

func TestServiceUpdatesExistingFindingAndResolvesMissing(t *testing.T) {
	now := time.Now().UTC().AddDate(0, 0, -14)
	repo := &fakeRepo{
		resources: []waste.Resource{{
			Provider:       domain.ProviderOCI,
			ResourceID:     "volume-1",
			ResourceType:   waste.ResourceBlockVolume,
			LifecycleState: "AVAILABLE",
			TimeCreated:    &now,
		}},
		costs:         map[string]waste.CostSignal{"volume-1": {Last7dCost: 7, HasLast7d: true}},
		upsertOutcome: "updated",
	}
	service := NewService(waste.Config{Enabled: true}, repo, nil, nil)

	result, err := service.Run(context.Background())
	if err != nil {
		t.Fatalf("run waste detection: %v", err)
	}
	if result.FindingsCreated != 0 || result.FindingsUpdated != 1 {
		t.Fatalf("expected 0 created and 1 updated, got created=%d updated=%d", result.FindingsCreated, result.FindingsUpdated)
	}
	if len(repo.upserted) != 1 {
		t.Fatalf("expected one upserted finding, got %d", len(repo.upserted))
	}
	if _, ok := repo.resolvedSeen["volume-1|OCI_DETACHED_BLOCK_VOLUME"]; !ok {
		t.Fatal("expected detected finding key to be passed to resolver")
	}
}

func TestServiceAWSProviderSkipsCleanly(t *testing.T) {
	service := NewService(waste.Config{Enabled: true, Provider: domain.ProviderAWS}, &fakeRepo{}, nil, nil)
	result, err := service.Run(context.Background())
	if err != nil {
		t.Fatalf("expected AWS waste stub to skip without error, got %v", err)
	}
	if !result.Skipped {
		t.Fatal("expected AWS waste detection to be skipped")
	}
}

func TestServiceConcurrentRunReturnsActiveError(t *testing.T) {
	repo := &fakeRepo{blockList: make(chan struct{}), listStarted: make(chan struct{})}
	service := NewService(waste.Config{Enabled: true}, repo, nil, nil)
	done := make(chan error, 1)
	go func() {
		_, err := service.Run(context.Background())
		done <- err
	}()
	<-repo.listStarted

	_, err := service.Run(context.Background())
	if !errors.Is(err, ErrRunAlreadyActive) {
		t.Fatalf("expected active run error, got %v", err)
	}
	close(repo.blockList)
	if err := <-done; err != nil {
		t.Fatalf("first run should complete cleanly, got %v", err)
	}
}

type fakeRepo struct {
	resources     []waste.Resource
	relationships []waste.Relationship
	costs         map[string]waste.CostSignal
	upserted      []waste.Finding
	upsertOutcome string
	resolvedSeen  map[string]struct{}
	blockList     chan struct{}
	listStarted   chan struct{}
}

func (f *fakeRepo) UpsertCloudResources(context.Context, []waste.Resource) error { return nil }
func (f *fakeRepo) ReplaceCloudRelationships(context.Context, domain.Provider, []waste.Relationship) error {
	return nil
}
func (f *fakeRepo) ListCloudResources(context.Context, domain.Provider) ([]waste.Resource, error) {
	if f.blockList != nil {
		close(f.listStarted)
		<-f.blockList
	}
	return f.resources, nil
}
func (f *fakeRepo) ListCloudRelationships(context.Context, domain.Provider) ([]waste.Relationship, error) {
	return f.relationships, nil
}
func (f *fakeRepo) GetResourceCostSignal(_ context.Context, _ domain.Provider, resourceID string, _ time.Time) (waste.CostSignal, error) {
	if f.costs == nil {
		return waste.CostSignal{}, nil
	}
	return f.costs[resourceID], nil
}
func (f *fakeRepo) UpsertWasteFinding(_ context.Context, finding waste.Finding) (string, error) {
	f.upserted = append(f.upserted, finding)
	if f.upsertOutcome != "" {
		return f.upsertOutcome, nil
	}
	return "created", nil
}
func (f *fakeRepo) ResolveMissingWasteFindings(_ context.Context, _ domain.Provider, _ []string, seen map[string]struct{}) (int, error) {
	f.resolvedSeen = seen
	return 0, nil
}
func (f *fakeRepo) ListWasteFindings(context.Context, waste.FindingFilters) ([]waste.Finding, error) {
	return nil, nil
}
func (f *fakeRepo) GetWasteFinding(context.Context, int64) (waste.Finding, error) {
	return waste.Finding{}, nil
}
func (f *fakeRepo) UpdateWasteFindingStatus(context.Context, int64, string) (waste.Finding, error) {
	return waste.Finding{}, nil
}
