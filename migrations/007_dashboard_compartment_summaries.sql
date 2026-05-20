-- Add OCI compartment dimensions to precomputed dashboard summaries.
-- This keeps Grafana compartment panels API-backed and avoids raw cost scans.

ALTER TABLE daily_cost_summaries
    ADD COLUMN IF NOT EXISTS compartment_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS compartment_name TEXT NOT NULL DEFAULT '';

ALTER TABLE weekly_cost_summaries
    ADD COLUMN IF NOT EXISTS compartment_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS compartment_name TEXT NOT NULL DEFAULT '';

ALTER TABLE monthly_cost_summaries
    ADD COLUMN IF NOT EXISTS compartment_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS compartment_name TEXT NOT NULL DEFAULT '';

ALTER TABLE daily_cost_summaries DROP CONSTRAINT IF EXISTS daily_cost_summaries_pkey;
ALTER TABLE daily_cost_summaries
    ADD PRIMARY KEY (period_start, provider, account_id, compartment_id, compartment_name, service, category, region, currency);

ALTER TABLE weekly_cost_summaries DROP CONSTRAINT IF EXISTS weekly_cost_summaries_pkey;
ALTER TABLE weekly_cost_summaries
    ADD PRIMARY KEY (period_start, provider, account_id, compartment_id, compartment_name, service, category, region, currency);

ALTER TABLE monthly_cost_summaries DROP CONSTRAINT IF EXISTS monthly_cost_summaries_pkey;
ALTER TABLE monthly_cost_summaries
    ADD PRIMARY KEY (period_start, provider, account_id, compartment_id, compartment_name, service, category, region, currency);

CREATE INDEX IF NOT EXISTS idx_daily_cost_summaries_compartment_range
    ON daily_cost_summaries (compartment_name, period_start DESC);

CREATE INDEX IF NOT EXISTS idx_daily_cost_summaries_region_range
    ON daily_cost_summaries (region, period_start DESC);

CREATE INDEX IF NOT EXISTS idx_weekly_cost_summaries_compartment_range
    ON weekly_cost_summaries (compartment_name, period_start DESC);

CREATE INDEX IF NOT EXISTS idx_monthly_cost_summaries_compartment_range
    ON monthly_cost_summaries (compartment_name, period_start DESC);
