CREATE TABLE IF NOT EXISTS billing_canonical
(
    date Date,
    provider LowCardinality(String),
    account_id String,
    service LowCardinality(String),
    region LowCardinality(String),
    resource_id String,
    currency LowCardinality(String),
    cost Float64,
    usage_amount Float64,
    usage_unit LowCardinality(String),
    tags_json String,
    source_object String,
    ingested_at DateTime DEFAULT now()
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(date)
ORDER BY (date, provider, account_id, service, region, resource_id);

CREATE TABLE IF NOT EXISTS anomalies
(
    date Date,
    provider LowCardinality(String),
    account_id String,
    service LowCardinality(String),
    baseline Float64,
    actual Float64,
    z_score Float64,
    percent_deviation Float64,
    moving_average_delta Float64,
    severity LowCardinality(String),
    created_at DateTime DEFAULT now()
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(date)
ORDER BY (date, provider, account_id, service, severity);

CREATE VIEW IF NOT EXISTS billing_daily_mv AS
SELECT
    date,
    provider,
    account_id,
    service,
    sum(cost) AS total_cost,
    sum(usage_amount) AS total_usage
FROM billing_canonical
GROUP BY date, provider, account_id, service;
