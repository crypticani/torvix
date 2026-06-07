package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/crypticani/torvix/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) GetAIEnrichment(ctx context.Context, entityType string, entityID int64) (domain.AIEnrichment, bool, error) {
	return scanAIEnrichment(r.db.QueryRow(ctx, `
		SELECT entity_type, entity_id, input_hash, provider, model, status,
		       summary, likely_cause, business_impact, recommended_actions,
		       priority, confidence, error, generated_at, updated_at
		FROM ai_enrichments
		WHERE entity_type = $1 AND entity_id = $2
	`, entityType, entityID))
}

func (r *Repository) GetAIEnrichmentByInput(ctx context.Context, entityType, inputHash, provider, model string) (domain.AIEnrichment, bool, error) {
	return scanAIEnrichment(r.db.QueryRow(ctx, `
		SELECT entity_type, entity_id, input_hash, provider, model, status,
		       summary, likely_cause, business_impact, recommended_actions,
		       priority, confidence, error, generated_at, updated_at
		FROM ai_enrichments
		WHERE entity_type = $1
		  AND input_hash = $2
		  AND provider = $3
		  AND model = $4
		  AND status = 'completed'
		ORDER BY updated_at DESC
		LIMIT 1
	`, entityType, inputHash, provider, model))
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAIEnrichment(row rowScanner) (domain.AIEnrichment, bool, error) {
	var enrichment domain.AIEnrichment
	var actions []byte
	var generatedAt sql.NullTime
	err := row.Scan(
		&enrichment.EntityType,
		&enrichment.EntityID,
		&enrichment.InputHash,
		&enrichment.Provider,
		&enrichment.Model,
		&enrichment.Status,
		&enrichment.Summary,
		&enrichment.LikelyCause,
		&enrichment.BusinessImpact,
		&actions,
		&enrichment.Priority,
		&enrichment.Confidence,
		&enrichment.Error,
		&generatedAt,
		&enrichment.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
			return domain.AIEnrichment{}, false, nil
		}
		return domain.AIEnrichment{}, false, err
	}
	if generatedAt.Valid {
		enrichment.GeneratedAt = &generatedAt.Time
	}
	if err := json.Unmarshal(actions, &enrichment.RecommendedActions); err != nil {
		return domain.AIEnrichment{}, false, fmt.Errorf("decode AI recommended actions: %w", err)
	}
	return enrichment, true, nil
}

type nullableAIEnrichment struct {
	provider       sql.NullString
	model          sql.NullString
	status         sql.NullString
	summary        sql.NullString
	likelyCause    sql.NullString
	businessImpact sql.NullString
	actions        []byte
	priority       sql.NullString
	confidence     sql.NullFloat64
	generatedAt    sql.NullTime
	updatedAt      sql.NullTime
}

func (n *nullableAIEnrichment) scanTargets() []any {
	return []any{
		&n.provider,
		&n.model,
		&n.status,
		&n.summary,
		&n.likelyCause,
		&n.businessImpact,
		&n.actions,
		&n.priority,
		&n.confidence,
		&n.generatedAt,
		&n.updatedAt,
	}
}

func (n *nullableAIEnrichment) value(entityType string, entityID int64) (*domain.AIEnrichment, error) {
	if !n.status.Valid {
		return nil, nil
	}
	enrichment := &domain.AIEnrichment{
		EntityType:     entityType,
		EntityID:       entityID,
		Provider:       n.provider.String,
		Model:          n.model.String,
		Status:         n.status.String,
		Summary:        n.summary.String,
		LikelyCause:    n.likelyCause.String,
		BusinessImpact: n.businessImpact.String,
		Priority:       n.priority.String,
		Confidence:     n.confidence.Float64,
		UpdatedAt:      n.updatedAt.Time,
	}
	if n.generatedAt.Valid {
		enrichment.GeneratedAt = &n.generatedAt.Time
	}
	if len(n.actions) > 0 {
		if err := json.Unmarshal(n.actions, &enrichment.RecommendedActions); err != nil {
			return nil, fmt.Errorf("decode AI recommended actions: %w", err)
		}
	}
	if enrichment.RecommendedActions == nil {
		enrichment.RecommendedActions = []string{}
	}
	return enrichment, nil
}

func (r *Repository) UpsertAIEnrichment(ctx context.Context, enrichment domain.AIEnrichment) error {
	actions, err := json.Marshal(enrichment.RecommendedActions)
	if err != nil {
		return fmt.Errorf("encode AI recommended actions: %w", err)
	}
	if enrichment.UpdatedAt.IsZero() {
		enrichment.UpdatedAt = time.Now().UTC()
	}
	_, err = r.db.Exec(ctx, `
		INSERT INTO ai_enrichments
			(entity_type, entity_id, input_hash, provider, model, status, summary, likely_cause,
			 business_impact, recommended_actions, priority, confidence, error, generated_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11, $12, $13, $14, $15)
		ON CONFLICT (entity_type, entity_id)
		DO UPDATE SET
			input_hash = EXCLUDED.input_hash,
			provider = EXCLUDED.provider,
			model = EXCLUDED.model,
			status = EXCLUDED.status,
			summary = EXCLUDED.summary,
			likely_cause = EXCLUDED.likely_cause,
			business_impact = EXCLUDED.business_impact,
			recommended_actions = EXCLUDED.recommended_actions,
			priority = EXCLUDED.priority,
			confidence = EXCLUDED.confidence,
			error = EXCLUDED.error,
			generated_at = EXCLUDED.generated_at,
			updated_at = EXCLUDED.updated_at
	`, enrichment.EntityType, enrichment.EntityID, enrichment.InputHash, enrichment.Provider, enrichment.Model,
		enrichment.Status, enrichment.Summary, enrichment.LikelyCause, enrichment.BusinessImpact, actions,
		enrichment.Priority, enrichment.Confidence, enrichment.Error, enrichment.GeneratedAt, enrichment.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert AI enrichment: %w", err)
	}
	return nil
}
