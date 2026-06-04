# Torvix Deployment

Torvix has two Compose entry points:

- `docker-compose.dev.yml`: full laptop/dev stack with Torvix, TimescaleDB, Prometheus, and Grafana.
- `docker-compose.prod.yml`: Torvix application only. Use this when production already has PostgreSQL/TimescaleDB, Prometheus, and Grafana.

Do not put real OCI credentials, database passwords, alert webhooks, or SMTP passwords in tracked files. Use local ignored config files under `configs/`.

## Development Setup

The development setup is self-contained and starts all dependencies.

1. Create the local config and Compose env file:

   ```bash
   cp configs/config.example.yaml configs/config.yaml
   cp .env.example .env
   ```

2. Update `configs/config.yaml` with local provider settings, and update `.env` with any Compose/container environment variables such as AWS credentials or port overrides. The Compose files load `.env` into the Torvix container with `env_file`; if `.env` is absent, YAML defaults still apply.

3. Start the stack:

   ```bash
   docker compose -f docker-compose.dev.yml up --build
   ```

   The Makefile alias is:

   ```bash
   make compose-dev-up
   ```

   To run Torvix on a different host-network port, set the app bind port:

   ```bash
   TORVIX_HTTP_PORT=18080 docker compose -f docker-compose.dev.yml up --build
   ```

   If you change the dev API port, also update `deploy/prometheus.yml` so the bundled Prometheus scrapes the same port.

4. Open the local services:

   - Torvix API: `http://localhost:8080`
   - Grafana: `http://localhost:3000`
   - Prometheus: `http://localhost:9090`
   - PostgreSQL/TimescaleDB: `localhost:5432`

5. Stop and remove local volumes:

   ```bash
   make compose-dev-down
   ```

The dev Grafana datasource provisioning expects:

- Prometheus datasource UID: `Prometheus`
- Torvix API datasource UID: `TorvixAPI`
- PostgreSQL datasource UID: `PostgreSQL` for local inspection only

The bundled dashboard uses `TorvixAPI` and `Prometheus`. It does not query PostgreSQL directly.

Torvix is daily operational FinOps tooling, not long-term archival billing warehousing. The default lifecycle is:

```yaml
ingestion:
  lookback_days: 30
  retention_days: 90
  compression_after_days: 7
```

Raw `cost_records` older than 90 days are removed by TimescaleDB retention and lifecycle maintenance. Precomputed dashboard summary and anomaly tables are refreshed after ingestion and pruned to the same 90-day horizon. Forecast rows are regenerated for the current forward-looking 7-day horizon and old forecast rows are pruned.

Object-level selection and `processed_report_files` dedupe reduce unnecessary OCI downloads. For broad prefixes such as `reports/`, Torvix narrows OCI proprietary cost report selection to `reports/cost-csv/`, seeks near the recent Object Storage metadata window, and processes the bounded candidate set newest-first. OCI numeric suffixes are not authoritative billing-period recency signals, and record-level filtering is still required because a recently modified billing export can contain historical usage rows. Torvix filters each parsed record by `ingestion.lookback_days` before insertion, so old rows are reported as `records_skipped_old` and never rely on retention cleanup to disappear. If selected reports produce zero rows inside the lookback window, `max_zero_yield_files` stops the run before it can spend minutes parsing historical data.

## Known Upgrade Issue

The latest release may fail during migration 011 on existing TimescaleDB deployments where `cost_records` has columnstore/compression enabled. A patch is implemented and currently under verification. Existing users with compressed/columnstore hypertables should avoid upgrading until the patch release is published.

## Production Setup

Use production Compose when PostgreSQL/TimescaleDB, Prometheus, and Grafana are managed outside the Torvix Compose stack.

1. Create the production config and Compose env file:

   ```bash
   cp configs/config.prod.example.yaml configs/config.prod.yaml
   cp .env.example .env
   ```

2. Set the production PostgreSQL/TimescaleDB DSN:

   ```yaml
   db:
     dsn: "postgres://torvix:replace_with_password@postgres.example.internal:5432/torvix?sslmode=require"
   ```

   If PostgreSQL runs on the Docker host, use:

   ```yaml
   db:
     dsn: "postgres://torvix:replace_with_password@host.docker.internal:5432/torvix?sslmode=disable"
   ```

3. Set production provider credentials in `configs/config.prod.yaml` and/or `.env`. OCI uses `providers.oci`; AWS uses `providers.aws` plus the AWS SDK credential environment. The production Compose file loads `.env` into the Torvix container with `env_file`; if `.env` is absent, only YAML/default values are used.

4. Configure any alerting targets under `reporting.webhooks`.
   The production example includes disabled placeholders for Slack, Microsoft Teams, Telegram, Discord, and SMTP email. Keep only the targets you use, replace placeholder secrets, set the correct `currency`, and set `enabled: true`.

   The default report scheduler uses `Asia/Kolkata`:

   ```yaml
   reporting:
     timezone: "Asia/Kolkata"
     daily_report_cron: "0 14 * * *"
     weekly_report_cron: "0 15 * * 1"
     require_complete_ingestion: true
     daily_report_target_lag_days: 1
   ```

   Daily reports run once per day at 2:00 PM IST and report day-1 data. Weekly reports run at 3:00 PM IST every Monday and cover the previous full Monday-to-Sunday week. If required daily data is not present, Torvix skips the report instead of sending an incomplete alert.

   The same values can be overridden with `TORVIX_REPORT_TIMEZONE`, `TORVIX_DAILY_REPORT_CRON`, `TORVIX_WEEKLY_REPORT_CRON`, `TORVIX_REPORT_REQUIRE_COMPLETE_INGESTION`, and `TORVIX_DAILY_REPORT_TARGET_LAG_DAYS`. Existing `CLOUDPULSE_*` report env vars are accepted only as lower-priority compatibility fallbacks.

   Waste detection runs independently from billing ingestion and defaults to OCI-only Phase 1 detection once per day:

   ```yaml
   waste:
     detection_enabled: true
     provider: "oci"
     scan_interval_hours: 24
     min_resource_age_days: 7
     stopped_instance_min_days: 3
     min_cost_threshold: 0
     high_monthly_threshold: 50
     enable_tag_exclusions: true
   ```

   The same values can be overridden with `TORVIX_WASTE_DETECTION_ENABLED`, `TORVIX_WASTE_PROVIDER`, `TORVIX_WASTE_SCAN_INTERVAL_HOURS`, `TORVIX_WASTE_MIN_RESOURCE_AGE_DAYS`, `TORVIX_WASTE_STOPPED_INSTANCE_MIN_DAYS`, `TORVIX_WASTE_MIN_COST_THRESHOLD`, `TORVIX_WASTE_HIGH_MONTHLY_THRESHOLD`, `TORVIX_WASTE_ENABLE_TAG_EXCLUSIONS`, and `TORVIX_WASTE_EXCLUSION_TAG_KEYS`.

5. Start only the Torvix app:

   ```bash
   docker compose -f docker-compose.prod.yml up --build -d
   ```

   The Makefile alias is:

   ```bash
   make compose-prod-up
   ```

6. Validate the app:

   ```bash
   curl http://localhost:8080/healthz
   curl http://localhost:8080/metrics
   ```

The production Compose file uses host networking and does not publish Docker ports. The app binds to `:8080` by default. Override the actual app listener with:

```bash
TORVIX_HTTP_PORT=18080 docker compose -f docker-compose.prod.yml up --build -d
```

Then validate with:

```bash
curl http://localhost:18080/healthz
curl http://localhost:18080/metrics
```

The config file can also manage the listener:

```yaml
http:
  address: ":18080"
```

Runtime precedence is:

1. `TORVIX_HTTP_ADDRESS`, for example `0.0.0.0:18080`
2. `TORVIX_HTTP_PORT`, for example `18080`
3. `TORVIX_API_PORT`, for example `18080`
4. `http.address` in the YAML config
5. default `:8080`

The production Compose healthcheck checks `http://127.0.0.1:${TORVIX_HTTP_PORT:-8080}/healthz` and also understands `TORVIX_API_PORT`. If you use `TORVIX_HTTP_ADDRESS`, the healthcheck derives the port from that value when no port variable is set.

If you change only `http.address` in `configs/config.prod.yaml`, also set `TORVIX_HTTP_PORT` to the same port or update the healthcheck command in `docker-compose.prod.yml`. Compose cannot read the mounted YAML value into its healthcheck automatically.

Torvix writes file-only JSON logs and does not emit normal application logs to stdout. Logs are split by subsystem into `app.log`, `http.log`, `ingestion.log`, `db.log`, `oci.log`, `aws.log`, `scheduler.log`, `alerting.log`, and `waste.log`. The bundled Compose files mount `./logs` to `/app/logs`; set `TORVIX_LOG_DIR=/app/logs` or keep `logging.dir: logs` while the container runs from `/app`.

Logging runtime controls:

```bash
TORVIX_LOG_LEVEL=debug
TORVIX_LOG_RETENTION_DAYS=14
TORVIX_LOG_DIR=/app/logs
```

Torvix deletes `.log` files in the configured log directory whose modification time is older than the retention window.

## Configure AWS

AWS defaults to CUR 2.0 / Data Export files in S3. This matches OCI's bucket-based billing export model and avoids Cost Explorer API request costs. Cost Explorer remains available as an explicit `cost_explorer` mode for quick testing, debugging, or manual fallback.

Torvix groups AWS costs by Region, Linked Account, and Service; Linked Account is the AWS billing scope equivalent for v1. VPC is intentionally not used as the base scope because many AWS charges are global, account-level, support-level, service-level, or otherwise not tied to a VPC.

### CUR/S3 Mode

Create an S3 bucket for billing exports, create an AWS Data Export / CUR 2.0 export, choose CSV gzip, set a prefix, and wait for AWS to write the first report. Torvix currently supports `csv` and `csv_gzip`; Parquet support is planned.

Add the minimum S3 read-only policy to the AWS principal used by Torvix:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "TorvixReadAwsBillingExports",
      "Effect": "Allow",
      "Action": [
        "s3:ListBucket"
      ],
      "Resource": "arn:aws:s3:::YOUR_BILLING_BUCKET",
      "Condition": {
        "StringLike": {
          "s3:prefix": [
            "YOUR_CUR_PREFIX/*",
            "YOUR_CUR_PREFIX"
          ]
        }
      }
    },
    {
      "Sid": "TorvixGetAwsBillingExportObjects",
      "Effect": "Allow",
      "Action": [
        "s3:GetObject"
      ],
      "Resource": "arn:aws:s3:::YOUR_BILLING_BUCKET/YOUR_CUR_PREFIX/*"
    }
  ]
}
```

Enable AWS in YAML:

```yaml
providers:
  aws:
    enabled: true
    ingestion_mode: "cur_s3"
    region: "us-east-1"
    cur_bucket: "your-billing-bucket"
    cur_prefix: "your-prefix/"
    cur_region: "us-east-1"
    cur_format: "csv_gzip"
    cur_lookback_days: 3
    cur_report_lag_days: 2
```

Or enable it through environment variables:

```bash
TORVIX_AWS_ENABLED=true
TORVIX_AWS_INGESTION_MODE=cur_s3
AWS_ACCESS_KEY_ID=replace_with_access_key
AWS_SECRET_ACCESS_KEY=replace_with_secret_key
AWS_REGION=us-east-1
TORVIX_AWS_CUR_BUCKET=your-billing-bucket
TORVIX_AWS_CUR_PREFIX=your-prefix/
TORVIX_AWS_CUR_REGION=us-east-1
TORVIX_AWS_CUR_FORMAT=csv_gzip
TORVIX_AWS_CUR_LOOKBACK_DAYS=3
TORVIX_AWS_CUR_REPORT_LAG_DAYS=2
```

Defaults:

- `TORVIX_AWS_ENABLED=false`
- `TORVIX_AWS_INGESTION_MODE=cur_s3`
- `AWS_REGION=us-east-1`
- `TORVIX_AWS_CUR_REGION` defaults to `AWS_REGION`
- `TORVIX_AWS_CUR_FORMAT=csv_gzip`
- `TORVIX_AWS_CUR_LOOKBACK_DAYS=3`
- `TORVIX_AWS_CUR_REPORT_LAG_DAYS=2`

For local contributor testing without AWS access, use a sanitized local CUR CSV or gzip file:

```bash
TORVIX_AWS_ENABLED=true
TORVIX_AWS_INGESTION_MODE=cur_s3
TORVIX_AWS_CUR_LOCAL_PATH=./testdata/aws/cur-sample.csv.gz
```

CUR records are stored with `provider='aws'`, `billing_scope_type='linked_account'`, and `record_type='cur_line_item'`. Re-reading the same export rows uses a deterministic `source_record_hash`, so ingestion updates existing rows instead of duplicating them. With the default lookback, an ingestion on `2026-06-01` reprocesses recent billing exports covering `2026-05-29`, `2026-05-30`, and `2026-05-31`. With the default AWS CUR report lag, the stable AWS daily report date on `2026-06-01` is `2026-05-30`.

### Optional Cost Explorer Mode

Cost Explorer mode is optional and must be selected explicitly:

```bash
TORVIX_AWS_ENABLED=true
TORVIX_AWS_INGESTION_MODE=cost_explorer
AWS_ACCESS_KEY_ID=replace_with_access_key
AWS_SECRET_ACCESS_KEY=replace_with_secret_key
AWS_REGION=us-east-1
TORVIX_AWS_COST_METRIC=UnblendedCost
```

Cost Explorer mode requires:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "TorvixCostExplorerReadOnly",
      "Effect": "Allow",
      "Action": [
        "ce:GetCostAndUsage",
        "ce:GetDimensionValues"
      ],
      "Resource": "*"
    }
  ]
}
```

Cost Explorer defaults:

- `AWS_REGION=us-east-1`
- `TORVIX_AWS_COST_METRIC=UnblendedCost`
- `TORVIX_AWS_LOOKBACK_DAYS=3`
- `TORVIX_AWS_REPORT_LAG_DAYS=2`

AWS Cost Explorer can mark recent data as estimated. Torvix stores that estimated flag in `raw_metadata`. Cost Explorer mode stores overlapping AWS query results separately and uses `linked_account_service` for general totals while using `region_service` for region views to avoid double-counting.

Useful AWS dashboard checks:

```bash
curl "http://localhost:8080/api/v1/dashboard/overview?provider=aws"
curl "http://localhost:8080/api/v1/dashboard/cost-by-region?provider=aws"
curl "http://localhost:8080/api/v1/dashboard/cost-by-scope?provider=aws"
curl "http://localhost:8080/api/v1/dashboard/drilldown?provider=aws"
```

Future AWS cost attribution should use CUR resource IDs where available, inventory enrichment, tags, cost categories, account mappings, and optional VPC-to-project mapping. Do not assume all AWS costs can map to VPC. A one-project-one-VPC model can be useful for project attribution where it exists, but services such as S3, Route 53, CloudFront, IAM, Support, and some data transfer need tag, cost-category, account-level, manual, or unallocated handling.

Resource limits can be tuned with:

```bash
TORVIX_CPU_LIMIT=1.0 TORVIX_MEMORY_LIMIT=512M docker compose -f docker-compose.prod.yml up --build -d
```

## Connect Production Prometheus

Torvix exposes Prometheus metrics at:

```text
http://<torvix-host>:<torvix-port>/metrics
```

Add this scrape job to your existing Prometheus configuration:

```yaml
scrape_configs:
  - job_name: torvix
    metrics_path: /metrics
    scrape_interval: 60s
    static_configs:
      - targets:
          - torvix.example.internal:8080
```

An example snippet is available at `deploy/prometheus.prod-scrape.example.yml`.

Keep the target port aligned with the Torvix listener. For example, if production runs with `TORVIX_HTTP_PORT=18080` or `http.address: ":18080"`, scrape `torvix.example.internal:18080` instead of `torvix.example.internal:8080`. The `60s` scrape interval is intentional because cost data changes on ingestion/report cadence, not every few seconds; lower it only if you need faster app health or ingestion-failure detection.

After reloading Prometheus, verify the target is up:

```promql
up{job="torvix"}
```

Useful Torvix metrics include:

```promql
torvix_processed_records_total
torvix_collector_runs_total
torvix_ingestion_duration_seconds_count
torvix_ingestion_failures_total
torvix_records_deleted_total
torvix_compressed_chunks_total
```

Cost values belong in PostgreSQL-backed Torvix APIs, not Prometheus labels. Prometheus should carry operational health metrics only: ingestion duration, files processed, records inserted, failures, skipped old files, records pruned, compressed chunks, and API/runtime status.

If `metrics.cost_stats_enabled` is enabled, Torvix also exposes coarse aggregate cost gauges from dashboard summary API calls:

```promql
torvix_cost_total
torvix_cost_services
torvix_cost_anomalies
```

These metrics only use a low-cardinality `window` label. Do not add service, account, resource, tag, source object, or raw billing dimensions as Prometheus labels.

## Import Dashboard Into Production Grafana

Torvix ships Grafana dashboard JSON files for provider cost views and waste findings:

- `dashboards/torvix-oci-finops-dashboard.json`
- `dashboards/torvix-aws-finops-dashboard.json`
- `dashboards/torvix-waste-dashboard.json`

The files can be pasted directly into a separate production Grafana import flow after the required datasources are configured. Local development and production both use these same dashboard JSON files. The local PostgreSQL datasource is provisioned only for direct developer inspection.

PostgreSQL must remain private in production. Do not expose the TimescaleDB port to Grafana users or public networks, and do not provision a production PostgreSQL datasource for dashboards. Production Grafana should read only from Torvix API endpoints and Prometheus.

The dashboards expect these Grafana datasource UIDs:

- `Prometheus`: your production Prometheus datasource.
- `TorvixAPI`: an Infinity datasource that points at the Torvix API.

The OCI dashboard production API endpoints are:

```text
GET /api/v1/dashboard/overview?provider=oci
GET /api/v1/dashboard/cost-timeseries?provider=oci
GET /api/v1/dashboard/cost-by-category?provider=oci
GET /api/v1/dashboard/cost-by-service?provider=oci
GET /api/v1/dashboard/cost-by-provider?provider=oci
GET /api/v1/dashboard/cost-by-compartment?provider=oci
GET /api/v1/dashboard/cost-by-region?provider=oci
GET /api/v1/dashboard/oci-cost-summary
GET /api/v1/dashboard/oci-cost-drivers
GET /api/v1/dashboard/anomalies?provider=oci
GET /api/v1/dashboard/ingestion-status
```

The AWS dashboard production API endpoints are:

```text
GET /api/v1/dashboard/overview?provider=aws
GET /api/v1/dashboard/cost-timeseries?provider=aws
GET /api/v1/dashboard/cost-by-region?provider=aws
GET /api/v1/dashboard/cost-by-scope?provider=aws
GET /api/v1/dashboard/cost-by-service?provider=aws
GET /api/v1/dashboard/drilldown?provider=aws
GET /api/v1/dashboard/anomalies?provider=aws
```

The waste dashboard production API endpoints are:

```text
GET /api/v1/waste/summary
GET /api/v1/waste/findings
GET /api/v1/waste/rules
```

The range endpoints accept `from=YYYY-MM-DD` or RFC3339 timestamps and `to=YYYY-MM-DD` or RFC3339 timestamps. Cost time series accepts `window=daily|weekly|monthly`. Service, region, scope, and compartment breakdowns accept `limit=15`. The OCI dashboard uses Region -> Compartment -> Service drill-down variables. The AWS dashboard uses Region -> Account/Scope -> Service drill-down variables. `All` means the matching filter is not applied. `Top OCI Cost Drivers` returns Region, Compartment, Service, Total Cost, and percent of the filtered total. Anomalies accepts `severity=low|medium|high`. The waste dashboard shows provider-selectable open findings, estimated monthly waste, top findings, and rule metadata.

Dashboard APIs read precomputed tables and return metadata with `retention_days`, `source: "precomputed"`, and an empty `data` array plus a clear message when the requested range is outside the retained 90-day window.

### Configure The Infinity Plugin

Install the Grafana Infinity datasource plugin before importing or provisioning the dashboard:

```bash
grafana cli plugins install yesoreyeram-infinity-datasource
```

For Grafana Docker, install it at container startup:

```yaml
environment:
  GF_PLUGINS_PREINSTALL_SYNC: "yesoreyeram-infinity-datasource"
```

Create an Infinity datasource with:

- Name: `Torvix API`
- UID: `TorvixAPI`
- Type: `yesoreyeram-infinity-datasource`
- URL: your Torvix API base URL, for example `https://torvix.example.internal`
- Access: `Server` or `Proxy`

If Torvix dashboard API auth is enabled, configure the Infinity datasource to send:

```text
Authorization: Bearer <torvix_grafana_api_bearer_token>
```

The bundled dashboard variables use Infinity backend JSON queries. If the datasource UID is not exactly `TorvixAPI`, map it during import or edit the JSON before import.

### Option 1: Grafana UI Import

1. In Grafana, go to **Dashboards -> New -> Import**.
2. Confirm the Infinity datasource plugin is installed and that a datasource with UID `TorvixAPI` exists.
3. Upload or paste the dashboard JSON file you want to import from `dashboards/`.
4. If Grafana asks for datasources, map:
   - `Prometheus` to your production Prometheus datasource.
   - `TorvixAPI` to your Torvix API datasource.

If your existing datasource UIDs are different and Grafana does not prompt for mapping, either create datasource aliases with the UIDs above or edit the JSON before import.

### Option 2: Grafana Provisioning

Copy the dashboard JSON files from `dashboards/` to your Grafana dashboard provisioning path and configure a provider similar to:

```yaml
apiVersion: 1

providers:
  - name: torvix
    folder: Torvix
    type: file
    disableDeletion: false
    editable: true
    options:
      path: /var/lib/grafana/dashboards
```

Create or update Grafana datasource provisioning so the UIDs match. A starter file is available at `docker/grafana/provisioning/datasources/datasources.prod.yml.example`:

```yaml
apiVersion: 1

datasources:
  - name: Prometheus
    uid: Prometheus
    type: prometheus
    access: proxy
    url: http://prometheus.example.internal:9090
    isDefault: true

  - name: Torvix API
    uid: TorvixAPI
    type: yesoreyeram-infinity-datasource
    access: proxy
    url: https://torvix.example.internal
    jsonData:
      auth_method: "bearerToken"
      httpHeaderName1: "Authorization"
    secureJsonData:
      httpHeaderValue1: "Bearer replace_with_torvix_grafana_token"
```

Restart or reload Grafana provisioning after copying the files.

Enable the Torvix Grafana API auth placeholder in production config:

```yaml
grafana:
  api_auth:
    enabled: true
    bearer_token: "replace_with_long_random_token"
```

The same value can be supplied with `TORVIX_GRAFANA_API_BEARER_TOKEN`. When auth is enabled, production Grafana must send `Authorization: Bearer <token>` to `/api/v1/dashboard/*`.

## Verify Dashboard Data After Ingestion

Use this sequence when cost panels are empty. It checks each layer in order instead of changing the dashboard cosmetically:

```bash
psql "$DATABASE_URL" -c "SELECT count(*) FROM cost_records;"
psql "$DATABASE_URL" -c "SELECT count(*) FROM daily_cost_summaries;"
psql "$DATABASE_URL" -c "SELECT count(*) FROM weekly_cost_summaries;"
psql "$DATABASE_URL" -c "SELECT count(*) FROM monthly_cost_summaries;"
psql "$DATABASE_URL" -c "SELECT count(*) FROM cost_anomalies;"
```

```bash
curl "http://localhost:8080/api/v1/dashboard/overview?provider=oci"
curl "http://localhost:8080/api/v1/dashboard/cost-timeseries?window=daily&provider=oci&from=2026-05-01&to=2026-05-31"
curl "http://localhost:8080/api/v1/dashboard/cost-by-region?provider=oci&from=2026-05-01&to=2026-05-31"
curl "http://localhost:8080/api/v1/dashboard/cost-by-compartment?provider=oci&region=ap-mumbai-1&from=2026-05-01&to=2026-05-31&limit=15"
curl "http://localhost:8080/api/v1/dashboard/cost-by-service?provider=oci&region=ap-mumbai-1&compartment=production&from=2026-05-01&to=2026-05-31&limit=15"
curl "http://localhost:8080/api/v1/dashboard/oci-cost-drivers?region=ap-mumbai-1&compartment=production&from=2026-05-01&to=2026-05-31&limit=15"
curl "http://localhost:8080/api/v1/dashboard/anomalies?provider=oci&from=2026-05-01&to=2026-05-31"
curl "http://localhost:8080/api/v1/dashboard/ingestion-status"
```

If `cost_records` has rows but `daily_cost_summaries` is empty, rerun ingestion after this release so the post-ingest analysis step executes. Dashboards intentionally do not scan raw billing rows. In the current lifecycle, `POST /api/v1/ingest` is the supported path: ingest, normalize, store raw records, recompute summaries, recompute anomalies, recompute forecasts, prune retained data, then serve dashboard APIs from precomputed tables.

Ingestion status counters distinguish these stages:

- `records_parsed`: rows read from downloaded billing reports.
- `records_within_lookback`: parsed rows whose usage timestamp is inside `ingestion.lookback_days`.
- `records_skipped_old`: parsed rows skipped before insertion because they are older than the lookback cutoff.
- `records_inserted`: records actually inserted into `cost_records`.

When all downloaded rows are historical, expect `records_inserted: 0`. In that case dashboard APIs should be empty because there is genuinely no recent retained billing data, not because PostgreSQL immediately pruned misleading inserts.

## Anomaly Detection

Torvix anomaly detection is deterministic. It does not use AI/ML today.

- Dimensions: provider, account, service, category, and region.
- Baseline: trailing 7 daily summary rows from `daily_cost_summaries`.
- Minimum absolute delta: `1.00`.
- Minimum percentage deviation: `30%`.
- Optional statistical threshold: z-score `2`.
- High severity: `50%` deviation or z-score `3`; otherwise matching anomalies are medium severity.

Each stored anomaly includes observed cost, expected cost, absolute delta, percentage delta, severity, method, and an explanation such as:

```text
OCI Storage daily spend was 82.0% above its trailing baseline: observed 18.40, expected 10.11.
```

## Operational Notes

- Production Compose does not run PostgreSQL, Prometheus, or Grafana.
- Torvix applies SQL migrations on startup, so the configured database user needs permissions to create tables, indexes, Timescale hypertables, compression policies, and retention policies.
- Keep `configs/config.prod.yaml` outside git. It is ignored by `.gitignore`.
- The app should be reachable by Prometheus over the network path configured in the scrape target.
- The database should be reachable only by Torvix and database administration paths, not by production Grafana.
