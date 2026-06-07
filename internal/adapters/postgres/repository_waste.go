package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/crypticani/torvix/internal/domain"
	"github.com/crypticani/torvix/internal/waste"
)

func (r *Repository) StartCloudInventoryRun(ctx context.Context, run waste.InventoryRun) (string, error) {
	if strings.TrimSpace(run.ID) == "" {
		run.ID = newInventoryRunID()
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}
	if run.Status == "" {
		run.Status = "running"
	}
	metadata, err := json.Marshal(nonNilAnyMap(run.Metadata))
	if err != nil {
		return "", fmt.Errorf("marshal inventory run metadata: %w", err)
	}
	_, err = r.db.Exec(ctx, `
		INSERT INTO cloud_inventory_runs (id, provider, region, scope_id, status, started_at, metadata)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5, $6, $7::jsonb)
	`, run.ID, run.Provider, run.Region, run.ScopeID, run.Status, run.StartedAt, metadata)
	if err != nil {
		return "", fmt.Errorf("start cloud inventory run: %w", err)
	}
	return run.ID, nil
}

func (r *Repository) CompleteCloudInventoryRun(ctx context.Context, runID, status, errMessage string) error {
	if status == "" {
		status = "success"
	}
	_, err := r.db.Exec(ctx, `
		UPDATE cloud_inventory_runs
		SET status = $2,
		    completed_at = NOW(),
		    error = NULLIF($3, '')
		WHERE id = $1
	`, runID, status, errMessage)
	if err != nil {
		return fmt.Errorf("complete cloud inventory run: %w", err)
	}
	return nil
}

func (r *Repository) MarkMissingCloudResourcesInactive(ctx context.Context, provider domain.Provider, region, runID string) (int, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE cloud_resources
		SET active = FALSE,
		    missing_since = COALESCE(missing_since, NOW()),
		    inactive_at = NOW()
		WHERE provider = $1
		  AND ($2 = '' OR COALESCE(region, '') = $2)
		  AND active = TRUE
		  AND (last_seen_run_id IS NULL OR last_seen_run_id <> $3)
	`, provider, region, runID)
	if err != nil {
		return 0, fmt.Errorf("mark missing cloud resources inactive: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (r *Repository) UpsertCloudResources(ctx context.Context, resources []waste.Resource) error {
	if len(resources) == 0 {
		return nil
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	for _, resource := range resources {
		tags, err := json.Marshal(nonNilStringMap(resource.Tags))
		if err != nil {
			return fmt.Errorf("marshal resource tags: %w", err)
		}
		raw, err := json.Marshal(nonNilAnyMap(resource.Raw))
		if err != nil {
			return fmt.Errorf("marshal resource raw metadata: %w", err)
		}
		firstSeenAt := resource.FirstSeenAt
		if firstSeenAt.IsZero() {
			firstSeenAt = time.Now().UTC()
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO cloud_resources
				(provider, resource_id, resource_name, resource_type, region, scope_id, scope_name, lifecycle_state, availability_domain, time_created, tags, raw, first_seen_at, last_seen_at, active, last_seen_run_id, missing_since, inactive_at)
			VALUES ($1, $2, NULLIF($3, ''), $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''), $10, $11::jsonb, $12::jsonb, $13, NOW(), TRUE, NULLIF($14, ''), NULL, NULL)
			ON CONFLICT (provider, resource_id)
			DO UPDATE SET
				resource_name = EXCLUDED.resource_name,
				resource_type = EXCLUDED.resource_type,
				region = EXCLUDED.region,
				scope_id = EXCLUDED.scope_id,
				scope_name = EXCLUDED.scope_name,
				lifecycle_state = EXCLUDED.lifecycle_state,
				availability_domain = EXCLUDED.availability_domain,
				time_created = EXCLUDED.time_created,
				tags = EXCLUDED.tags,
				raw = EXCLUDED.raw,
				last_seen_at = NOW(),
				active = TRUE,
				last_seen_run_id = EXCLUDED.last_seen_run_id,
				missing_since = NULL,
				inactive_at = NULL
		`, resource.Provider, resource.ResourceID, resource.ResourceName, resource.ResourceType, resource.Region, resource.ScopeID, resource.ScopeName, resource.LifecycleState, resource.AvailabilityDomain, nullableTime(resource.TimeCreated), tags, raw, firstSeenAt, resource.LastSeenRunID)
		if err != nil {
			return fmt.Errorf("upsert cloud resource %s: %w", resource.ResourceID, err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

func (r *Repository) ReplaceCloudRelationships(ctx context.Context, provider domain.Provider, relationships []waste.Relationship) error {
	scoped := make(map[waste.RelationshipScope][]waste.Relationship)
	for _, rel := range relationships {
		scope := waste.RelationshipScope{
			Provider:         provider,
			Region:           rel.Region,
			ScopeID:          rel.ScopeID,
			RelationshipType: rel.RelationshipType,
		}
		scoped[scope] = append(scoped[scope], rel)
	}
	for scope, scopedRelationships := range scoped {
		if err := r.ReplaceCloudRelationshipsScoped(ctx, scope, scopedRelationships); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ReplaceCloudRelationshipsScoped(ctx context.Context, scope waste.RelationshipScope, relationships []waste.Relationship) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if _, err = tx.Exec(ctx, `
		DELETE FROM cloud_resource_relationships
		WHERE provider = $1
		  AND relationship_type = $2
		  AND COALESCE(region, '') = COALESCE(NULLIF($3, ''), '')
		  AND COALESCE(scope_id, '') = COALESCE(NULLIF($4, ''), '')
	`, scope.Provider, scope.RelationshipType, scope.Region, scope.ScopeID); err != nil {
		return fmt.Errorf("delete existing cloud relationships: %w", err)
	}
	for _, rel := range relationships {
		raw, err := json.Marshal(nonNilAnyMap(rel.Raw))
		if err != nil {
			return fmt.Errorf("marshal relationship raw metadata: %w", err)
		}
		detectedAt := rel.DetectedAt
		if detectedAt.IsZero() {
			detectedAt = time.Now().UTC()
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO cloud_resource_relationships
				(provider, source_resource_id, target_resource_id, relationship_type, region, scope_id, detected_at, raw)
			VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8::jsonb)
			ON CONFLICT (provider, source_resource_id, target_resource_id, relationship_type)
			DO UPDATE SET
				region = EXCLUDED.region,
				scope_id = EXCLUDED.scope_id,
				detected_at = EXCLUDED.detected_at,
				raw = EXCLUDED.raw
		`, rel.Provider, rel.SourceResourceID, rel.TargetResourceID, rel.RelationshipType, rel.Region, rel.ScopeID, detectedAt, raw)
		if err != nil {
			return fmt.Errorf("upsert cloud relationship %s -> %s: %w", rel.SourceResourceID, rel.TargetResourceID, err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

func (r *Repository) ListCloudResources(ctx context.Context, provider domain.Provider) ([]waste.Resource, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, provider, resource_id, COALESCE(resource_name, ''), resource_type, COALESCE(region, ''), COALESCE(scope_id, ''), COALESCE(scope_name, ''),
		       COALESCE(lifecycle_state, ''), COALESCE(availability_domain, ''), time_created, tags, raw, first_seen_at, last_seen_at
		FROM cloud_resources
		WHERE provider = $1
		  AND active = TRUE
		ORDER BY resource_type, resource_name, resource_id
	`, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var resources []waste.Resource
	for rows.Next() {
		resource, err := scanCloudResource(rows)
		if err != nil {
			return nil, err
		}
		resources = append(resources, resource)
	}
	return resources, rows.Err()
}

func (r *Repository) ListCloudRelationships(ctx context.Context, provider domain.Provider) ([]waste.Relationship, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, provider, source_resource_id, target_resource_id, relationship_type, COALESCE(region, ''), COALESCE(scope_id, ''), detected_at, raw
		FROM cloud_resource_relationships
		WHERE provider = $1
	`, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var relationships []waste.Relationship
	for rows.Next() {
		var rel waste.Relationship
		var raw []byte
		if err := rows.Scan(&rel.ID, &rel.Provider, &rel.SourceResourceID, &rel.TargetResourceID, &rel.RelationshipType, &rel.Region, &rel.ScopeID, &rel.DetectedAt, &raw); err != nil {
			return nil, err
		}
		rel.Raw = map[string]any{}
		_ = json.Unmarshal(raw, &rel.Raw)
		relationships = append(relationships, rel)
	}
	return relationships, rows.Err()
}

func (r *Repository) GetResourceCostSignal(ctx context.Context, provider domain.Provider, resourceID string, now time.Time) (waste.CostSignal, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var signal waste.CostSignal
	var last7, last30 sql.NullFloat64
	var count7, count30 int64
	var currency sql.NullString
	err := r.db.QueryRow(ctx, `
		SELECT
			SUM(cost::double precision) FILTER (WHERE "timestamp" >= $3)::double precision AS last_7d_cost,
			SUM(cost::double precision)::double precision AS last_30d_cost,
			COUNT(*) FILTER (WHERE "timestamp" >= $3) AS count_7d,
			COUNT(*) AS count_30d,
			MAX(currency) AS currency
		FROM cost_records
		WHERE cloud_provider = $1
		  AND resource_id = $2
		  AND "timestamp" >= $4
	`, provider, resourceID, now.AddDate(0, 0, -7), now.AddDate(0, 0, -30)).Scan(&last7, &last30, &count7, &count30, &currency)
	if err != nil {
		return signal, err
	}
	if last7.Valid {
		signal.Last7dCost = last7.Float64
	}
	if last30.Valid {
		signal.Last30dCost = last30.Float64
	}
	signal.HasLast7d = count7 > 0
	signal.HasLast30d = count30 > 0
	if currency.Valid {
		signal.Currency = currency.String
	}
	return signal, nil
}

func (r *Repository) UpsertWasteFinding(ctx context.Context, finding waste.Finding) (string, error) {
	evidence, err := json.Marshal(nonNilAnyMap(finding.Evidence))
	if err != nil {
		return "", fmt.Errorf("marshal waste finding evidence: %w", err)
	}
	var inserted bool
	detectedAt := finding.DetectedAt
	if detectedAt.IsZero() {
		detectedAt = time.Now().UTC()
	}
	err = r.db.QueryRow(ctx, `
		INSERT INTO waste_findings
			(provider, resource_id, resource_name, resource_type, region, scope_id, scope_name, service, rule_id, severity, confidence, estimated_monthly_waste, currency, summary, recommendation, evidence, status, detected_at, last_seen_at)
		VALUES ($1, $2, NULLIF($3, ''), $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), $9, $10, $11, $12, $13, $14, $15, $16::jsonb, 'open', $17, NOW())
		ON CONFLICT (provider, resource_id, rule_id)
		DO UPDATE SET
			resource_name = EXCLUDED.resource_name,
			resource_type = EXCLUDED.resource_type,
			region = EXCLUDED.region,
			scope_id = EXCLUDED.scope_id,
			scope_name = EXCLUDED.scope_name,
			service = EXCLUDED.service,
			severity = EXCLUDED.severity,
			confidence = EXCLUDED.confidence,
			estimated_monthly_waste = EXCLUDED.estimated_monthly_waste,
			currency = EXCLUDED.currency,
			summary = EXCLUDED.summary,
			recommendation = EXCLUDED.recommendation,
			evidence = EXCLUDED.evidence,
			status = CASE
				WHEN waste_findings.status IN ('resolved', 'fixed') THEN 'open'
				ELSE waste_findings.status
			END,
			last_seen_at = NOW(),
			resolved_at = CASE
				WHEN waste_findings.status IN ('resolved', 'fixed') THEN NULL
				ELSE waste_findings.resolved_at
			END
		RETURNING xmax = 0
	`, finding.Provider, finding.ResourceID, finding.ResourceName, finding.ResourceType, finding.Region, finding.ScopeID, finding.ScopeName, finding.Service, finding.RuleID, finding.Severity, finding.Confidence, finding.EstimatedMonthlyWaste, valueOrDefault(finding.Currency, "USD"), finding.Summary, finding.Recommendation, evidence, detectedAt).Scan(&inserted)
	if err != nil {
		return "", err
	}
	if inserted {
		return "created", nil
	}
	return "updated", nil
}

func (r *Repository) ResolveMissingWasteFindings(ctx context.Context, provider domain.Provider, ruleIDs []string, seen map[string]struct{}) (int, error) {
	seenKeys := make([]string, 0, len(seen))
	for key := range seen {
		seenKeys = append(seenKeys, key)
	}
	tag, err := r.db.Exec(ctx, `
		UPDATE waste_findings
		SET status = 'resolved', resolved_at = NOW(), last_seen_at = NOW()
		WHERE provider = $1
		  AND rule_id = ANY($2::text[])
		  AND status = 'open'
		  AND NOT ((resource_id || '|' || rule_id) = ANY($3::text[]))
	`, provider, ruleIDs, seenKeys)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (r *Repository) ListWasteFindings(ctx context.Context, filters waste.FindingFilters) ([]waste.Finding, error) {
	query := `
		SELECT w.id, w.provider, w.resource_id, COALESCE(w.resource_name, ''), w.resource_type, COALESCE(w.region, ''), COALESCE(w.scope_id, ''), COALESCE(w.scope_name, ''),
		       COALESCE(w.service, ''), w.rule_id, w.severity, w.confidence::double precision, w.estimated_monthly_waste::double precision, w.currency, w.summary, w.recommendation,
		       w.evidence, w.status, w.detected_at, w.last_seen_at, w.resolved_at,
		       ai.provider, ai.model, ai.status, ai.summary, ai.likely_cause, ai.business_impact,
		       ai.recommended_actions, ai.priority, ai.confidence, ai.generated_at, ai.updated_at
		FROM waste_findings w
		LEFT JOIN ai_enrichments ai
		  ON ai.entity_type = 'waste_finding'
		 AND ai.entity_id = w.id
		 AND ai.status = 'completed'`
	where, args := wasteFindingWhere(filters)
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY w.estimated_monthly_waste DESC NULLS LAST, w.confidence DESC, w.last_seen_at DESC"
	if filters.Limit <= 0 {
		filters.Limit = 100
	}
	if filters.Limit > 1000 {
		filters.Limit = 1000
	}
	args = append(args, filters.Limit, filters.Offset)
	query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWasteFindings(rows, true)
}

func (r *Repository) GetWasteFinding(ctx context.Context, id int64) (waste.Finding, error) {
	rows, err := r.db.Query(ctx, `
		SELECT w.id, w.provider, w.resource_id, COALESCE(w.resource_name, ''), w.resource_type, COALESCE(w.region, ''), COALESCE(w.scope_id, ''), COALESCE(w.scope_name, ''),
		       COALESCE(w.service, ''), w.rule_id, w.severity, w.confidence::double precision, w.estimated_monthly_waste::double precision, w.currency, w.summary, w.recommendation,
		       w.evidence, w.status, w.detected_at, w.last_seen_at, w.resolved_at,
		       ai.provider, ai.model, ai.status, ai.summary, ai.likely_cause, ai.business_impact,
		       ai.recommended_actions, ai.priority, ai.confidence, ai.generated_at, ai.updated_at
		FROM waste_findings w
		LEFT JOIN ai_enrichments ai
		  ON ai.entity_type = 'waste_finding'
		 AND ai.entity_id = w.id
		 AND ai.status = 'completed'
		WHERE w.id = $1
	`, id)
	if err != nil {
		return waste.Finding{}, err
	}
	defer rows.Close()
	findings, err := scanWasteFindings(rows, true)
	if err != nil {
		return waste.Finding{}, err
	}
	if len(findings) == 0 {
		return waste.Finding{}, pgx.ErrNoRows
	}
	return findings[0], nil
}

func (r *Repository) UpdateWasteFindingStatus(ctx context.Context, id int64, status string) (waste.Finding, error) {
	resolvedAtExpr := "resolved_at"
	if status == waste.StatusResolved || status == waste.StatusFixed {
		resolvedAtExpr = "NOW()"
	}
	rows, err := r.db.Query(ctx, fmt.Sprintf(`
		UPDATE waste_findings
		SET status = $2,
		    resolved_at = %s,
		    last_seen_at = NOW()
		WHERE id = $1
		RETURNING id, provider, resource_id, COALESCE(resource_name, ''), resource_type, COALESCE(region, ''), COALESCE(scope_id, ''), COALESCE(scope_name, ''),
		          COALESCE(service, ''), rule_id, severity, confidence::double precision, estimated_monthly_waste::double precision, currency, summary, recommendation,
		          evidence, status, detected_at, last_seen_at, resolved_at
	`, resolvedAtExpr), id, status)
	if err != nil {
		return waste.Finding{}, err
	}
	defer rows.Close()
	findings, err := scanWasteFindings(rows, false)
	if err != nil {
		return waste.Finding{}, err
	}
	if len(findings) == 0 {
		return waste.Finding{}, pgx.ErrNoRows
	}
	return findings[0], nil
}

func wasteFindingWhere(filters waste.FindingFilters) ([]string, []any) {
	var where []string
	var args []any
	add := func(sql string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(sql, len(args)))
	}
	if filters.Provider != "" {
		add("w.provider = $%d", filters.Provider)
	}
	if filters.Region != "" {
		add("w.region = $%d", filters.Region)
	}
	if filters.ScopeID != "" {
		add("w.scope_id = $%d", filters.ScopeID)
	}
	if filters.ScopeName != "" {
		add("w.scope_name = $%d", filters.ScopeName)
	}
	if filters.Service != "" {
		add("w.service = $%d", filters.Service)
	}
	if filters.ResourceType != "" {
		add("w.resource_type = $%d", filters.ResourceType)
	}
	if filters.RuleID != "" {
		add("w.rule_id = $%d", filters.RuleID)
	}
	if filters.Severity != "" {
		add("w.severity = $%d", filters.Severity)
	}
	if filters.Status != "" {
		add("w.status = $%d", filters.Status)
	}
	if filters.MinConfidence != nil {
		add("w.confidence >= $%d", *filters.MinConfidence)
	}
	if filters.MinEstimatedMonthlyWaste != nil {
		add("w.estimated_monthly_waste >= $%d", *filters.MinEstimatedMonthlyWaste)
	}
	return where, args
}

func scanCloudResource(rows pgx.Rows) (waste.Resource, error) {
	var resource waste.Resource
	var timeCreated sql.NullTime
	var tags, raw []byte
	if err := rows.Scan(&resource.ID, &resource.Provider, &resource.ResourceID, &resource.ResourceName, &resource.ResourceType, &resource.Region, &resource.ScopeID, &resource.ScopeName, &resource.LifecycleState, &resource.AvailabilityDomain, &timeCreated, &tags, &raw, &resource.FirstSeenAt, &resource.LastSeenAt); err != nil {
		return resource, err
	}
	if timeCreated.Valid {
		resource.TimeCreated = &timeCreated.Time
	}
	resource.Tags = map[string]string{}
	_ = json.Unmarshal(tags, &resource.Tags)
	resource.Raw = map[string]any{}
	_ = json.Unmarshal(raw, &resource.Raw)
	return resource, nil
}

func scanWasteFindings(rows pgx.Rows, includeAI bool) ([]waste.Finding, error) {
	var findings []waste.Finding
	for rows.Next() {
		var finding waste.Finding
		var estimate sql.NullFloat64
		var evidence []byte
		var resolvedAt sql.NullTime
		targets := []any{&finding.ID, &finding.Provider, &finding.ResourceID, &finding.ResourceName, &finding.ResourceType, &finding.Region, &finding.ScopeID, &finding.ScopeName, &finding.Service, &finding.RuleID, &finding.Severity, &finding.Confidence, &estimate, &finding.Currency, &finding.Summary, &finding.Recommendation, &evidence, &finding.Status, &finding.DetectedAt, &finding.LastSeenAt, &resolvedAt}
		var enrichment nullableAIEnrichment
		if includeAI {
			targets = append(targets, enrichment.scanTargets()...)
		}
		if err := rows.Scan(targets...); err != nil {
			return nil, err
		}
		if estimate.Valid {
			finding.EstimatedMonthlyWaste = &estimate.Float64
		}
		finding.Evidence = map[string]any{}
		_ = json.Unmarshal(evidence, &finding.Evidence)
		if resolvedAt.Valid {
			finding.ResolvedAt = &resolvedAt.Time
		}
		if includeAI {
			var err error
			finding.AIEnrichment, err = enrichment.value(domain.AIEntityWaste, finding.ID)
			if err != nil {
				return nil, err
			}
		}
		findings = append(findings, finding)
	}
	return findings, rows.Err()
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

func nonNilStringMap(values map[string]string) map[string]string {
	if values == nil {
		return map[string]string{}
	}
	return values
}

func nonNilAnyMap(values map[string]any) map[string]any {
	if values == nil {
		return map[string]any{}
	}
	return values
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func newInventoryRunID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("run-%d", time.Now().UTC().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(b[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}
