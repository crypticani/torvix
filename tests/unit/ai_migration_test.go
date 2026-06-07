package unit

import (
	"os"
	"strings"
	"testing"
)

func TestAIEnrichmentMigrationIsAdditive(t *testing.T) {
	b, err := os.ReadFile("../../migrations/015_ai_enrichments.sql")
	if err != nil {
		t.Fatalf("read AI enrichment migration: %v", err)
	}
	migration := string(b)
	for _, expected := range []string{
		"CREATE TABLE IF NOT EXISTS ai_enrichments",
		"entity_type TEXT NOT NULL",
		"entity_id BIGINT NOT NULL",
		"input_hash TEXT NOT NULL",
		"recommended_actions JSONB NOT NULL DEFAULT '[]'::jsonb",
		"UNIQUE (entity_type, entity_id)",
		"CREATE INDEX IF NOT EXISTS idx_ai_enrichments_status_updated",
		"CREATE INDEX IF NOT EXISTS idx_ai_enrichments_input_reuse",
	} {
		if !strings.Contains(migration, expected) {
			t.Fatalf("migration missing %q", expected)
		}
	}
	for _, forbidden := range []string{"DROP TABLE", "TRUNCATE", "DELETE FROM cost_records", "DELETE FROM waste_findings"} {
		if strings.Contains(migration, forbidden) {
			t.Fatalf("AI migration must remain additive, found %q", forbidden)
		}
	}
}
