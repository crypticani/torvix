package aws

import (
	"context"
	"testing"

	"github.com/crypticani/torvix/internal/domain"
)

func TestWasteStubSkipsCleanly(t *testing.T) {
	result, err := NewWasteStub(nil).Sync(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !result.Skipped {
		t.Fatal("expected skipped result")
	}
	if result.Provider != domain.ProviderAWS {
		t.Fatalf("expected aws provider, got %s", result.Provider)
	}
}
