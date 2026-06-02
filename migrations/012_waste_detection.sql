CREATE TABLE IF NOT EXISTS cloud_resources
(
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    resource_name TEXT,
    resource_type TEXT NOT NULL,
    region TEXT,
    scope_id TEXT,
    scope_name TEXT,
    lifecycle_state TEXT,
    availability_domain TEXT,
    time_created TIMESTAMPTZ,
    tags JSONB NOT NULL DEFAULT '{}'::jsonb,
    raw JSONB NOT NULL DEFAULT '{}'::jsonb,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(provider, resource_id)
);

CREATE INDEX IF NOT EXISTS idx_cloud_resources_provider_type
    ON cloud_resources (provider, resource_type);

CREATE INDEX IF NOT EXISTS idx_cloud_resources_provider_region
    ON cloud_resources (provider, region);

CREATE INDEX IF NOT EXISTS idx_cloud_resources_provider_scope
    ON cloud_resources (provider, scope_id, scope_name);

CREATE INDEX IF NOT EXISTS idx_cloud_resources_tags_gin
    ON cloud_resources USING GIN (tags);

CREATE TABLE IF NOT EXISTS cloud_resource_relationships
(
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL,
    source_resource_id TEXT NOT NULL,
    target_resource_id TEXT NOT NULL,
    relationship_type TEXT NOT NULL,
    region TEXT,
    scope_id TEXT,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    raw JSONB NOT NULL DEFAULT '{}'::jsonb,
    UNIQUE(provider, source_resource_id, target_resource_id, relationship_type)
);

CREATE INDEX IF NOT EXISTS idx_cloud_resource_relationships_provider_source
    ON cloud_resource_relationships (provider, source_resource_id);

CREATE INDEX IF NOT EXISTS idx_cloud_resource_relationships_provider_target
    ON cloud_resource_relationships (provider, target_resource_id);

CREATE TABLE IF NOT EXISTS waste_findings
(
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    resource_name TEXT,
    resource_type TEXT NOT NULL,
    region TEXT,
    scope_id TEXT,
    scope_name TEXT,
    service TEXT,
    rule_id TEXT NOT NULL,
    severity TEXT NOT NULL,
    confidence NUMERIC(5,2) NOT NULL,
    estimated_monthly_waste NUMERIC(18,6),
    currency TEXT NOT NULL DEFAULT 'USD',
    summary TEXT NOT NULL,
    recommendation TEXT NOT NULL,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'open',
    detected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    UNIQUE(provider, resource_id, rule_id)
);

CREATE INDEX IF NOT EXISTS idx_waste_findings_provider_status
    ON waste_findings (provider, status, last_seen_at DESC);

CREATE INDEX IF NOT EXISTS idx_waste_findings_provider_rule
    ON waste_findings (provider, rule_id);

CREATE INDEX IF NOT EXISTS idx_waste_findings_provider_region
    ON waste_findings (provider, region);

CREATE INDEX IF NOT EXISTS idx_waste_findings_provider_scope
    ON waste_findings (provider, scope_id, scope_name);

CREATE INDEX IF NOT EXISTS idx_waste_findings_provider_service
    ON waste_findings (provider, service);

CREATE INDEX IF NOT EXISTS idx_waste_findings_provider_resource_type
    ON waste_findings (provider, resource_type);

CREATE INDEX IF NOT EXISTS idx_waste_findings_evidence_gin
    ON waste_findings USING GIN (evidence);
