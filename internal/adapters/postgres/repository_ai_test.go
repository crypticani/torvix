package postgres

import (
	"context"
	"testing"
	"time"

	pgxmock "github.com/pashagolub/pgxmock/v4"

	"github.com/crypticani/torvix/internal/domain"
	"github.com/crypticani/torvix/internal/waste"
)

func TestAIEnrichmentRoundTrip(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer mock.Close()

	now := time.Date(2026, 6, 7, 10, 0, 0, 0, time.UTC)
	mock.ExpectExec("INSERT INTO ai_enrichments").
		WithArgs(
			domain.AIEntityAnomaly, int64(42), "hash", "openai", "gpt-5.4-mini",
			domain.AIStatusCompleted, "summary", "cause", "impact", pgxmock.AnyArg(),
			"high", 0.8, "", pgxmock.AnyArg(), now,
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectQuery("FROM ai_enrichments").
		WithArgs(domain.AIEntityAnomaly, int64(42)).
		WillReturnRows(pgxmock.NewRows([]string{
			"entity_type", "entity_id", "input_hash", "provider", "model", "status",
			"summary", "likely_cause", "business_impact", "recommended_actions",
			"priority", "confidence", "error", "generated_at", "updated_at",
		}).AddRow(
			domain.AIEntityAnomaly, int64(42), "hash", "openai", "gpt-5.4-mini", domain.AIStatusCompleted,
			"summary", "cause", "impact", []byte(`["review ownership"]`),
			"high", 0.8, "", now, now,
		))
	mock.ExpectQuery("input_hash = \\$2").
		WithArgs(domain.AIEntityAnomaly, "hash", "openai", "gpt-5.4-mini").
		WillReturnRows(pgxmock.NewRows([]string{
			"entity_type", "entity_id", "input_hash", "provider", "model", "status",
			"summary", "likely_cause", "business_impact", "recommended_actions",
			"priority", "confidence", "error", "generated_at", "updated_at",
		}).AddRow(
			domain.AIEntityAnomaly, int64(42), "hash", "openai", "gpt-5.4-mini", domain.AIStatusCompleted,
			"summary", "cause", "impact", []byte(`["review ownership"]`),
			"high", 0.8, "", now, now,
		))

	repo := NewWithDB(mock)
	generatedAt := now
	value := domain.AIEnrichment{
		EntityType:         domain.AIEntityAnomaly,
		EntityID:           42,
		InputHash:          "hash",
		Provider:           "openai",
		Model:              "gpt-5.4-mini",
		Status:             domain.AIStatusCompleted,
		Summary:            "summary",
		LikelyCause:        "cause",
		BusinessImpact:     "impact",
		RecommendedActions: []string{"review ownership"},
		Priority:           "high",
		Confidence:         0.8,
		GeneratedAt:        &generatedAt,
		UpdatedAt:          now,
	}
	if err := repo.UpsertAIEnrichment(context.Background(), value); err != nil {
		t.Fatalf("UpsertAIEnrichment() error = %v", err)
	}
	got, found, err := repo.GetAIEnrichment(context.Background(), domain.AIEntityAnomaly, 42)
	if err != nil {
		t.Fatalf("GetAIEnrichment() error = %v", err)
	}
	if !found || got.Summary != "summary" || len(got.RecommendedActions) != 1 {
		t.Fatalf("unexpected enrichment: found=%v value=%+v", found, got)
	}
	reused, found, err := repo.GetAIEnrichmentByInput(context.Background(), domain.AIEntityAnomaly, "hash", "openai", "gpt-5.4-mini")
	if err != nil {
		t.Fatalf("GetAIEnrichmentByInput() error = %v", err)
	}
	if !found || reused.EntityID != 42 {
		t.Fatalf("unexpected reusable enrichment: found=%v value=%+v", found, reused)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDashboardAnomaliesIncludesCompletedAIEnrichment(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer mock.Close()

	now := time.Date(2026, 6, 7, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery("LEFT JOIN ai_enrichments").
		WithArgs(now.Add(-24*time.Hour), now).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "detected_at", "period_start", "provider", "account_id", "compartment_id", "compartment_name",
			"category", "service", "region", "currency", "observed_cost", "expected_cost", "absolute_delta",
			"percentage_delta", "direction", "severity", "detection_method", "explanation", "created_at",
			"ai_provider", "ai_model", "ai_status", "ai_summary", "ai_likely_cause", "ai_business_impact",
			"ai_actions", "ai_priority", "ai_confidence", "ai_generated_at", "ai_updated_at",
		}).AddRow(
			int64(9), now, now.Add(-12*time.Hour), "aws", "acct", "", "",
			"all", "EC2", "us-east-1", "USD", 150.0, 100.0, 50.0,
			50.0, "increase", "high", "trailing_7_day_baseline", "deterministic explanation", now,
			"openai", "gpt-5.4-mini", "completed", "AI summary", "usage changed", "review spend",
			[]byte(`["check deployment"]`), "high", 0.75, now, now,
		))

	repo := NewWithDB(mock)
	rows, err := repo.DashboardAnomalies(context.Background(), now.Add(-24*time.Hour), now, "")
	if err != nil {
		t.Fatalf("DashboardAnomalies() error = %v", err)
	}
	if len(rows) != 1 || rows[0].ID != 9 || rows[0].AIEnrichment == nil || rows[0].AIEnrichment.Summary != "AI summary" {
		t.Fatalf("unexpected anomaly response: %+v", rows)
	}
}

func TestWasteFindingsIncludesCompletedAIEnrichment(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer mock.Close()

	now := time.Date(2026, 6, 7, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery("LEFT JOIN ai_enrichments").
		WithArgs(10, 0).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "provider", "resource_id", "resource_name", "resource_type", "region", "scope_id", "scope_name",
			"service", "rule_id", "severity", "confidence", "estimated_monthly_waste", "currency", "summary",
			"recommendation", "evidence", "status", "detected_at", "last_seen_at", "resolved_at",
			"ai_provider", "ai_model", "ai_status", "ai_summary", "ai_likely_cause", "ai_business_impact",
			"ai_actions", "ai_priority", "ai_confidence", "ai_generated_at", "ai_updated_at",
		}).AddRow(
			int64(7), "oci", "volume-1", "data", "block_volume", "ap-mumbai-1", "scope", "production",
			"Block Storage", "OCI_DETACHED_BLOCK_VOLUME", "high", 0.9, 25.0, "USD", "Detached volume",
			"Review and delete if unused", []byte(`{"state":"AVAILABLE"}`), "open", now, now, nil,
			"openai", "gpt-5.4-mini", "completed", "AI waste summary", "attachment removed", "avoidable storage cost",
			[]byte(`["confirm owner"]`), "high", 0.8, now, now,
		))

	repo := NewWithDB(mock)
	rows, err := repo.ListWasteFindings(context.Background(), waste.FindingFilters{Limit: 10})
	if err != nil {
		t.Fatalf("ListWasteFindings() error = %v", err)
	}
	if len(rows) != 1 || rows[0].AIEnrichment == nil || rows[0].AIEnrichment.Summary != "AI waste summary" {
		t.Fatalf("unexpected waste response: %+v", rows)
	}
}
