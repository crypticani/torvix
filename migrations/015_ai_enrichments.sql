CREATE TABLE IF NOT EXISTS ai_enrichments (
    id BIGSERIAL PRIMARY KEY,
    entity_type TEXT NOT NULL,
    entity_id BIGINT NOT NULL,
    input_hash TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    status TEXT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    likely_cause TEXT NOT NULL DEFAULT '',
    business_impact TEXT NOT NULL DEFAULT '',
    recommended_actions JSONB NOT NULL DEFAULT '[]'::jsonb,
    priority TEXT NOT NULL DEFAULT '',
    confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    generated_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (entity_type, entity_id)
);

CREATE INDEX IF NOT EXISTS idx_ai_enrichments_status_updated
    ON ai_enrichments (status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_ai_enrichments_input_reuse
    ON ai_enrichments (entity_type, input_hash, provider, model, updated_at DESC)
    WHERE status = 'completed';
