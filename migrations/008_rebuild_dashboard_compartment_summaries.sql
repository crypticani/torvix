-- Rebuild dashboard summaries after adding OCI compartment dimensions.
--
-- Existing installations can have cost_records with compartment tags while
-- daily/weekly/monthly dashboard summaries still contain one blank compartment
-- bucket from an earlier refresh. Recompute the precomputed summaries so the
-- API-backed Grafana compartment panel reflects existing billing data without
-- requiring reports to be re-ingested.

DELETE FROM daily_cost_summaries;

WITH current_period AS (
    SELECT
        time_bucket(INTERVAL '1 day', "timestamp") AS period_start,
        cloud_provider AS provider,
        COALESCE(account_id, '') AS account_id,
        COALESCE(tags->>'oci_compartment_id', '') AS compartment_id,
        COALESCE(NULLIF(tags->>'oci_compartment_name', ''), tags->>'oci_compartment_id', account_id, '') AS compartment_name,
        COALESCE(service, 'unknown') AS service,
        COALESCE(category, 'uncategorized') AS category,
        COALESCE(region, '') AS region,
        COALESCE(currency, '') AS currency,
        COALESCE(SUM(cost), 0)::double precision AS total_cost
    FROM cost_records
    GROUP BY 1, 2, 3, 4, 5, 6, 7, 8, 9
),
previous_period AS (
    SELECT
        time_bucket(INTERVAL '1 day', "timestamp") + INTERVAL '1 day' AS period_start,
        cloud_provider AS provider,
        COALESCE(account_id, '') AS account_id,
        COALESCE(tags->>'oci_compartment_id', '') AS compartment_id,
        COALESCE(NULLIF(tags->>'oci_compartment_name', ''), tags->>'oci_compartment_id', account_id, '') AS compartment_name,
        COALESCE(service, 'unknown') AS service,
        COALESCE(category, 'uncategorized') AS category,
        COALESCE(region, '') AS region,
        COALESCE(currency, '') AS currency,
        COALESCE(SUM(cost), 0)::double precision AS total_cost
    FROM cost_records
    GROUP BY 1, 2, 3, 4, 5, 6, 7, 8, 9
)
INSERT INTO daily_cost_summaries
(period_start, period_end, provider, account_id, compartment_id, compartment_name, service, category, region, currency, total_cost, previous_period_cost, absolute_change, percentage_change, updated_at)
SELECT
    c.period_start,
    c.period_start + INTERVAL '1 day',
    c.provider,
    c.account_id,
    c.compartment_id,
    c.compartment_name,
    c.service,
    c.category,
    c.region,
    c.currency,
    c.total_cost,
    COALESCE(p.total_cost, 0)::double precision,
    (c.total_cost - COALESCE(p.total_cost, 0))::double precision,
    CASE
        WHEN COALESCE(p.total_cost, 0) > 0 THEN ((c.total_cost - p.total_cost) / p.total_cost) * 100
        WHEN c.total_cost > 0 THEN 100
        ELSE 0
    END::double precision,
    NOW()
FROM current_period c
LEFT JOIN previous_period p
  ON p.period_start = c.period_start
 AND p.provider = c.provider
 AND p.account_id = c.account_id
 AND p.compartment_id = c.compartment_id
 AND p.compartment_name = c.compartment_name
 AND p.service = c.service
 AND p.category = c.category
 AND p.region = c.region
 AND p.currency = c.currency;

DELETE FROM weekly_cost_summaries;

WITH current_period AS (
    SELECT
        time_bucket(INTERVAL '1 week', "timestamp") AS period_start,
        cloud_provider AS provider,
        COALESCE(account_id, '') AS account_id,
        COALESCE(tags->>'oci_compartment_id', '') AS compartment_id,
        COALESCE(NULLIF(tags->>'oci_compartment_name', ''), tags->>'oci_compartment_id', account_id, '') AS compartment_name,
        COALESCE(service, 'unknown') AS service,
        COALESCE(category, 'uncategorized') AS category,
        COALESCE(region, '') AS region,
        COALESCE(currency, '') AS currency,
        COALESCE(SUM(cost), 0)::double precision AS total_cost
    FROM cost_records
    GROUP BY 1, 2, 3, 4, 5, 6, 7, 8, 9
),
previous_period AS (
    SELECT
        time_bucket(INTERVAL '1 week', "timestamp") + INTERVAL '1 week' AS period_start,
        cloud_provider AS provider,
        COALESCE(account_id, '') AS account_id,
        COALESCE(tags->>'oci_compartment_id', '') AS compartment_id,
        COALESCE(NULLIF(tags->>'oci_compartment_name', ''), tags->>'oci_compartment_id', account_id, '') AS compartment_name,
        COALESCE(service, 'unknown') AS service,
        COALESCE(category, 'uncategorized') AS category,
        COALESCE(region, '') AS region,
        COALESCE(currency, '') AS currency,
        COALESCE(SUM(cost), 0)::double precision AS total_cost
    FROM cost_records
    GROUP BY 1, 2, 3, 4, 5, 6, 7, 8, 9
)
INSERT INTO weekly_cost_summaries
(period_start, period_end, provider, account_id, compartment_id, compartment_name, service, category, region, currency, total_cost, previous_period_cost, absolute_change, percentage_change, updated_at)
SELECT
    c.period_start,
    c.period_start + INTERVAL '1 week',
    c.provider,
    c.account_id,
    c.compartment_id,
    c.compartment_name,
    c.service,
    c.category,
    c.region,
    c.currency,
    c.total_cost,
    COALESCE(p.total_cost, 0)::double precision,
    (c.total_cost - COALESCE(p.total_cost, 0))::double precision,
    CASE
        WHEN COALESCE(p.total_cost, 0) > 0 THEN ((c.total_cost - p.total_cost) / p.total_cost) * 100
        WHEN c.total_cost > 0 THEN 100
        ELSE 0
    END::double precision,
    NOW()
FROM current_period c
LEFT JOIN previous_period p
  ON p.period_start = c.period_start
 AND p.provider = c.provider
 AND p.account_id = c.account_id
 AND p.compartment_id = c.compartment_id
 AND p.compartment_name = c.compartment_name
 AND p.service = c.service
 AND p.category = c.category
 AND p.region = c.region
 AND p.currency = c.currency;

DELETE FROM monthly_cost_summaries;

WITH current_period AS (
    SELECT
        time_bucket(INTERVAL '1 month', "timestamp") AS period_start,
        cloud_provider AS provider,
        COALESCE(account_id, '') AS account_id,
        COALESCE(tags->>'oci_compartment_id', '') AS compartment_id,
        COALESCE(NULLIF(tags->>'oci_compartment_name', ''), tags->>'oci_compartment_id', account_id, '') AS compartment_name,
        COALESCE(service, 'unknown') AS service,
        COALESCE(category, 'uncategorized') AS category,
        COALESCE(region, '') AS region,
        COALESCE(currency, '') AS currency,
        COALESCE(SUM(cost), 0)::double precision AS total_cost
    FROM cost_records
    GROUP BY 1, 2, 3, 4, 5, 6, 7, 8, 9
),
previous_period AS (
    SELECT
        time_bucket(INTERVAL '1 month', "timestamp") + INTERVAL '1 month' AS period_start,
        cloud_provider AS provider,
        COALESCE(account_id, '') AS account_id,
        COALESCE(tags->>'oci_compartment_id', '') AS compartment_id,
        COALESCE(NULLIF(tags->>'oci_compartment_name', ''), tags->>'oci_compartment_id', account_id, '') AS compartment_name,
        COALESCE(service, 'unknown') AS service,
        COALESCE(category, 'uncategorized') AS category,
        COALESCE(region, '') AS region,
        COALESCE(currency, '') AS currency,
        COALESCE(SUM(cost), 0)::double precision AS total_cost
    FROM cost_records
    GROUP BY 1, 2, 3, 4, 5, 6, 7, 8, 9
)
INSERT INTO monthly_cost_summaries
(period_start, period_end, provider, account_id, compartment_id, compartment_name, service, category, region, currency, total_cost, previous_period_cost, absolute_change, percentage_change, updated_at)
SELECT
    c.period_start,
    c.period_start + INTERVAL '1 month',
    c.provider,
    c.account_id,
    c.compartment_id,
    c.compartment_name,
    c.service,
    c.category,
    c.region,
    c.currency,
    c.total_cost,
    COALESCE(p.total_cost, 0)::double precision,
    (c.total_cost - COALESCE(p.total_cost, 0))::double precision,
    CASE
        WHEN COALESCE(p.total_cost, 0) > 0 THEN ((c.total_cost - p.total_cost) / p.total_cost) * 100
        WHEN c.total_cost > 0 THEN 100
        ELSE 0
    END::double precision,
    NOW()
FROM current_period c
LEFT JOIN previous_period p
  ON p.period_start = c.period_start
 AND p.provider = c.provider
 AND p.account_id = c.account_id
 AND p.compartment_id = c.compartment_id
 AND p.compartment_name = c.compartment_name
 AND p.service = c.service
 AND p.category = c.category
 AND p.region = c.region
 AND p.currency = c.currency;
