-- Precomputed dashboard analytics for production-safe Grafana panels.
-- Grafana reads these through Torvix HTTP APIs; it should not query
-- PostgreSQL directly in production.

CREATE TABLE IF NOT EXISTS daily_cost_summaries
(
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    provider TEXT NOT NULL,
    account_id TEXT NOT NULL DEFAULT '',
    compartment_id TEXT NOT NULL DEFAULT '',
    compartment_name TEXT NOT NULL DEFAULT '',
    billing_scope_type TEXT NOT NULL DEFAULT '',
    billing_scope_id TEXT NOT NULL DEFAULT '',
    billing_scope_name TEXT NOT NULL DEFAULT '',
    record_type TEXT NOT NULL DEFAULT '',
    service TEXT NOT NULL DEFAULT 'unknown',
    category TEXT NOT NULL DEFAULT 'uncategorized',
    region TEXT NOT NULL DEFAULT '',
    currency TEXT NOT NULL DEFAULT '',
    total_cost NUMERIC(20,8) NOT NULL DEFAULT 0,
    previous_period_cost NUMERIC(20,8) NOT NULL DEFAULT 0,
    absolute_change NUMERIC(20,8) NOT NULL DEFAULT 0,
    percentage_change DOUBLE PRECISION NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (period_start, provider, account_id, compartment_id, compartment_name, record_type, service, category, region, currency)
);

CREATE TABLE IF NOT EXISTS weekly_cost_summaries
(
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    provider TEXT NOT NULL,
    account_id TEXT NOT NULL DEFAULT '',
    compartment_id TEXT NOT NULL DEFAULT '',
    compartment_name TEXT NOT NULL DEFAULT '',
    billing_scope_type TEXT NOT NULL DEFAULT '',
    billing_scope_id TEXT NOT NULL DEFAULT '',
    billing_scope_name TEXT NOT NULL DEFAULT '',
    record_type TEXT NOT NULL DEFAULT '',
    service TEXT NOT NULL DEFAULT 'unknown',
    category TEXT NOT NULL DEFAULT 'uncategorized',
    region TEXT NOT NULL DEFAULT '',
    currency TEXT NOT NULL DEFAULT '',
    total_cost NUMERIC(20,8) NOT NULL DEFAULT 0,
    previous_period_cost NUMERIC(20,8) NOT NULL DEFAULT 0,
    absolute_change NUMERIC(20,8) NOT NULL DEFAULT 0,
    percentage_change DOUBLE PRECISION NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (period_start, provider, account_id, compartment_id, compartment_name, record_type, service, category, region, currency)
);

CREATE TABLE IF NOT EXISTS monthly_cost_summaries
(
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    provider TEXT NOT NULL,
    account_id TEXT NOT NULL DEFAULT '',
    compartment_id TEXT NOT NULL DEFAULT '',
    compartment_name TEXT NOT NULL DEFAULT '',
    billing_scope_type TEXT NOT NULL DEFAULT '',
    billing_scope_id TEXT NOT NULL DEFAULT '',
    billing_scope_name TEXT NOT NULL DEFAULT '',
    record_type TEXT NOT NULL DEFAULT '',
    service TEXT NOT NULL DEFAULT 'unknown',
    category TEXT NOT NULL DEFAULT 'uncategorized',
    region TEXT NOT NULL DEFAULT '',
    currency TEXT NOT NULL DEFAULT '',
    total_cost NUMERIC(20,8) NOT NULL DEFAULT 0,
    previous_period_cost NUMERIC(20,8) NOT NULL DEFAULT 0,
    absolute_change NUMERIC(20,8) NOT NULL DEFAULT 0,
    percentage_change DOUBLE PRECISION NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (period_start, provider, account_id, compartment_id, compartment_name, record_type, service, category, region, currency)
);

CREATE TABLE IF NOT EXISTS cost_anomalies
(
    id BIGSERIAL PRIMARY KEY,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    period_start TIMESTAMPTZ NOT NULL,
    provider TEXT NOT NULL,
    account_id TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT 'uncategorized',
    service TEXT NOT NULL DEFAULT 'unknown',
    region TEXT NOT NULL DEFAULT '',
    observed_cost NUMERIC(20,8) NOT NULL,
    expected_cost NUMERIC(20,8) NOT NULL,
    absolute_delta NUMERIC(20,8) NOT NULL,
    percentage_delta DOUBLE PRECISION NOT NULL,
    severity TEXT NOT NULL,
    detection_method TEXT NOT NULL,
    explanation TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS cost_forecasts
(
    forecast_date DATE NOT NULL,
    generated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    provider TEXT NOT NULL,
    account_id TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT 'uncategorized',
    service TEXT NOT NULL DEFAULT 'unknown',
    region TEXT NOT NULL DEFAULT '',
    forecast_cost NUMERIC(20,8) NOT NULL,
    confidence_low NUMERIC(20,8) NOT NULL,
    confidence_high NUMERIC(20,8) NOT NULL,
    method TEXT NOT NULL,
    PRIMARY KEY (forecast_date, provider, account_id, category, service, region, method)
);

CREATE INDEX IF NOT EXISTS idx_daily_cost_summaries_range_provider
    ON daily_cost_summaries (period_start DESC, provider, category, service);

CREATE INDEX IF NOT EXISTS idx_daily_cost_summaries_service_range
    ON daily_cost_summaries (service, period_start DESC);

CREATE INDEX IF NOT EXISTS idx_daily_cost_summaries_category_range
    ON daily_cost_summaries (category, period_start DESC);

CREATE INDEX IF NOT EXISTS idx_daily_cost_summaries_compartment_range
    ON daily_cost_summaries (compartment_name, period_start DESC);

CREATE INDEX IF NOT EXISTS idx_daily_cost_summaries_region_range
    ON daily_cost_summaries (region, period_start DESC);

CREATE INDEX IF NOT EXISTS idx_weekly_cost_summaries_range_provider
    ON weekly_cost_summaries (period_start DESC, provider, category, service);

CREATE INDEX IF NOT EXISTS idx_monthly_cost_summaries_range_provider
    ON monthly_cost_summaries (period_start DESC, provider, category, service);

CREATE INDEX IF NOT EXISTS idx_cost_anomalies_range_severity
    ON cost_anomalies (period_start DESC, severity, provider, category, service);

CREATE INDEX IF NOT EXISTS idx_cost_anomalies_service_range
    ON cost_anomalies (service, period_start DESC);

CREATE INDEX IF NOT EXISTS idx_cost_forecasts_date_provider
    ON cost_forecasts (forecast_date DESC, provider, category, service);

-- Existing installations may still have the old 180-day policy from earlier
-- migrations. Replace it with the supported operational default.
DO $$
BEGIN
    PERFORM remove_retention_policy('cost_records', if_exists => TRUE);
    PERFORM add_retention_policy('cost_records', INTERVAL '90 days', if_not_exists => TRUE);
END $$;
