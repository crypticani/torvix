package ai

import (
	"context"

	"github.com/crypticani/torvix/internal/domain"
)

type Request struct {
	Kind    string
	Context map[string]any
}

type Result struct {
	Summary            string   `json:"summary"`
	LikelyCause        string   `json:"likely_cause"`
	BusinessImpact     string   `json:"business_impact"`
	RecommendedActions []string `json:"recommended_actions"`
	Priority           string   `json:"priority"`
	Confidence         float64  `json:"confidence"`
}

type Client interface {
	Generate(ctx context.Context, request Request) (Result, error)
}

type Repository interface {
	GetAIEnrichment(ctx context.Context, entityType string, entityID int64) (domain.AIEnrichment, bool, error)
	GetAIEnrichmentByInput(ctx context.Context, entityType, inputHash, provider, model string) (domain.AIEnrichment, bool, error)
	UpsertAIEnrichment(ctx context.Context, enrichment domain.AIEnrichment) error
}
