CREATE TABLE IF NOT EXISTS cloud_inventory_runs
(
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    region TEXT,
    scope_id TEXT,
    status TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    error TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_cloud_inventory_runs_provider_status
    ON cloud_inventory_runs (provider, status, completed_at DESC);

CREATE INDEX IF NOT EXISTS idx_cloud_inventory_runs_provider_region
    ON cloud_inventory_runs (provider, region, scope_id, started_at DESC);

ALTER TABLE cloud_resources
    ADD COLUMN IF NOT EXISTS active BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS last_seen_run_id TEXT,
    ADD COLUMN IF NOT EXISTS missing_since TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS inactive_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_cloud_resources_provider_active
    ON cloud_resources (provider, active, resource_type);

CREATE INDEX IF NOT EXISTS idx_cloud_resources_last_seen_run
    ON cloud_resources (last_seen_run_id);

CREATE INDEX IF NOT EXISTS idx_cloud_resource_relationships_scope_type
    ON cloud_resource_relationships (provider, relationship_type, region, scope_id);
