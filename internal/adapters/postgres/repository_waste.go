package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/crypticani/torvix/internal/domain"
	"github.com/crypticani/torvix/internal/waste"
)

func (r *Repository) UpsertCloudResources(ctx context.Context, resources []waste.Resource) error {
	if len(resources) == 0 {
		return nil
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
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
				(provider, resource_id, resource_name, resource_type, region, scope_id, scope_name, lifecycle_state, availability_domain, time_created, tags, raw, first_seen_at, last_seen_at)
			VALUES ($1, $2, NULLIF($3, ''), $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''), $10, $11::jsonb, $12::jsonb, $13, NOW())
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
				last_seen_at = NOW()
		`, resource.Provider, resource.ResourceID, resource.ResourceName, resource.ResourceType, resource.Region, resource.ScopeID, resource.ScopeName, resource.LifecycleState, resource.AvailabilityDomain, nullableTime(resource.TimeCreated), tags, raw, firstSeenAt)
		if err != nil {
			return fmt.Errorf("upsert cloud resource %s: %w", resource.ResourceID, err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func (r *Repository) ReplaceCloudRelationships(ctx context.Context, provider domain.Provider, relationships []waste.Relationship) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if _, err = tx.Exec(ctx, `DELETE FROM cloud_resource_relationships WHERE provider = $1`, provider); err != nil {
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
	return nil
}

func (r *Repository) ListCloudResources(ctx context.Context, provider domain.Provider) ([]waste.Resource, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, provider, resource_id, COALESCE(resource_name, ''), resource_type, COALESCE(region, ''), COALESCE(scope_id, ''), COALESCE(scope_name, ''),
		       COALESCE(lifecycle_state, ''), COALESCE(availability_domain, ''), time_created, tags, raw, first_seen_at, last_seen_at
		FROM cloud_resources
		WHERE provider = $1
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
		SELECT id, provider, resource_id, COALESCE(resource_name, ''), resource_type, COALESCE(region, ''), COALESCE(scope_id, ''), COALESCE(scope_name, ''),
		       COALESCE(service, ''), rule_id, severity, confidence::double precision, estimated_monthly_waste::double precision, currency, summary, recommendation,
		       evidence, status, detected_at, last_seen_at, resolved_at
		FROM waste_findings`
	where, args := wasteFindingWhere(filters)
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY estimated_monthly_waste DESC NULLS LAST, confidence DESC, last_seen_at DESC"
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
	return scanWasteFindings(rows)
}

func (r *Repository) GetWasteFinding(ctx context.Context, id int64) (waste.Finding, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, provider, resource_id, COALESCE(resource_name, ''), resource_type, COALESCE(region, ''), COALESCE(scope_id, ''), COALESCE(scope_name, ''),
		       COALESCE(service, ''), rule_id, severity, confidence::double precision, estimated_monthly_waste::double precision, currency, summary, recommendation,
		       evidence, status, detected_at, last_seen_at, resolved_at
		FROM waste_findings
		WHERE id = $1
	`, id)
	if err != nil {
		return waste.Finding{}, err
	}
	defer rows.Close()
	findings, err := scanWasteFindings(rows)
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
	findings, err := scanWasteFindings(rows)
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
		add("provider = $%d", filters.Provider)
	}
	if filters.Region != "" {
		add("region = $%d", filters.Region)
	}
	if filters.ScopeID != "" {
		add("scope_id = $%d", filters.ScopeID)
	}
	if filters.ScopeName != "" {
		add("scope_name = $%d", filters.ScopeName)
	}
	if filters.Service != "" {
		add("service = $%d", filters.Service)
	}
	if filters.ResourceType != "" {
		add("resource_type = $%d", filters.ResourceType)
	}
	if filters.RuleID != "" {
		add("rule_id = $%d", filters.RuleID)
	}
	if filters.Severity != "" {
		add("severity = $%d", filters.Severity)
	}
	if filters.Status != "" {
		add("status = $%d", filters.Status)
	}
	if filters.MinConfidence != nil {
		add("confidence >= $%d", *filters.MinConfidence)
	}
	if filters.MinEstimatedMonthlyWaste != nil {
		add("estimated_monthly_waste >= $%d", *filters.MinEstimatedMonthlyWaste)
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

func scanWasteFindings(rows pgx.Rows) ([]waste.Finding, error) {
	var findings []waste.Finding
	for rows.Next() {
		var finding waste.Finding
		var estimate sql.NullFloat64
		var evidence []byte
		var resolvedAt sql.NullTime
		if err := rows.Scan(&finding.ID, &finding.Provider, &finding.ResourceID, &finding.ResourceName, &finding.ResourceType, &finding.Region, &finding.ScopeID, &finding.ScopeName, &finding.Service, &finding.RuleID, &finding.Severity, &finding.Confidence, &estimate, &finding.Currency, &finding.Summary, &finding.Recommendation, &evidence, &finding.Status, &finding.DetectedAt, &finding.LastSeenAt, &resolvedAt); err != nil {
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
