# Torvix

Find anomalies, waste, and hidden cloud spend.

Torvix is an open-source cloud cost intelligence and waste detection platform.

Torvix is an open-source FinOps platform for cloud cost visibility, anomaly detection, forecasting, and unused-resource detection across cloud providers.
The mascot concept is a futuristic anime-style cloud sentinel who watches infrastructure costs, detects anomalies, and exposes wasted resources before they impact the bill.

Torvix uses PostgreSQL with the TimescaleDB extension as its permanent and only database backend.
It is operational FinOps tooling, not long-term archival billing warehousing; the supported default historical horizon is 90 days.

## Capabilities

- OCI billing export ingestion, AWS CUR/Data Export ingestion, and optional AWS Cost Explorer ingestion
- Canonical multi-cloud normalization
- Daily, weekly, and monthly precomputed dashboard summaries
- Explainable anomaly detection using trailing baselines, percentage deviation, and optional z-score thresholds
- OCI Phase 1 unused-resource and waste detection with recommendation-only findings
- Rolling forecast generation
- Slack, Microsoft Teams, Telegram, Discord, and SMTP email report delivery
- Prometheus metrics and Grafana dashboards
- Separate Docker Compose files for full local development and production app-only deployment

## Architecture

```text
cmd/torvix               application entrypoint
internal/app                 bootstrap, DB wiring, migrations
internal/adapters/postgres   pgx repository and migration runner
internal/adapters/providers  cloud collectors, including OCI Object Storage, AWS CUR/S3, and AWS Cost Explorer
internal/core                collection, normalization, analytics, reporting
internal/ports               collector and repository contracts
migrations                   PostgreSQL + Timescale SQL migrations
configs                      YAML configuration
deploy                       Prometheus and cron examples
dashboards                   OCI, AWS, and waste Grafana dashboard JSON
```

## Storage Design

- `cost_records` is a Timescale hypertable partitioned on `timestamp`
- tags and provider metadata are stored in `JSONB`
- `daily_cost_summaries`, `weekly_cost_summaries`, `monthly_cost_summaries`, `cost_anomalies`, and `cost_forecasts` are precomputed after ingestion for dashboard APIs
- compression and retention policies are defined in migrations; raw records are retained for 90 days by default and compressed after 7 days
- `processed_report_files` tracks incremental billing ingestion and idempotency
- generic billing scope, project, network scope, resource, tag, and raw metadata fields keep provider records comparable while leaving room for future enrichment

Production dashboard flow:

```text
Grafana Infinity datasource -> Torvix dashboard APIs -> precomputed PostgreSQL/TimescaleDB analytics tables
Grafana Prometheus datasource -> Prometheus -> Torvix operational metrics
```

Production Grafana must not connect directly to PostgreSQL. Direct database access is kept for local developer inspection only.

## OCI Billing Ingestion

OCI support is production-oriented and first-class. The collector:

- authenticates with the official OCI Go SDK using an OCI config file and API keys
- lists OCI proprietary cost report objects from `reports/cost-csv/`
- seeks into the recent cost-report object range using Object Storage metadata, selects bounded candidates newest-first, and stops early after `max_zero_yield_files` consecutive reports contain zero rows inside the rolling lookback
- streams CSV and gzip-compressed exports
- tolerates schema drift through dynamic header matching
- normalizes OCI services into Compute, Storage, Networking, Database, Load Balancer, Monitoring, Security, and Kubernetes categories
- skips files already recorded in `processed_report_files`
- filters parsed records by the configured rolling lookback before insertion, so historical rows older than `ingestion.lookback_days` are counted as skipped and never inserted

Object selection is only an efficiency heuristic for reaching usable reports quickly when the bucket contains many historical files. For broad OCI prefixes such as `reports/`, Torvix narrows selection to `reports/cost-csv/`, uses metadata to seek near the configured lookback window, and processes the bounded candidate set newest-first. Torvix does not treat the auto-incrementing numeric report suffix as proof that a row belongs in the dashboard. Row-level lookback remains the source of truth for deciding which billing records are inserted, and `max_zero_yield_files` prevents long no-op runs through historical reports.

Oracle deprecated older usage reports on January 31, 2025, so the parser accepts both older usage-style headers and current OCI cost report layouts.

### OCI Setup

1. Create an OCI API key for a user with permission to read usage report objects.
2. Mount an OCI config file into the container, for example `/app/configs/oci_config`.
3. Set the OCI provider in `configs/config.yaml`:

```yaml
providers:
  oci:
    enabled: true
    namespace: "bling"
    bucket: "ocid1.tenancy.oc1..replace_with_tenancy_ocid"
    account: "ocid1.tenancy.oc1..replace_with_tenancy_ocid"
    prefix: ""
    config_file: "/app/configs/oci_config"
    config_profile: "DEFAULT"
    lookback_days: 7
```

Validate that reports are visible before starting ingestion:

```bash
oci os object list \
  --namespace-name bling \
  --bucket-name "$TENANCY_OCID" \
  --all
```

Trigger ingestion and validate results:

```bash
curl -X POST http://localhost:8080/api/v1/ingest
curl http://localhost:8080/api/v1/ingest/status/<job_id>
psql "$DATABASE_URL" -c "SELECT count(*) FROM cost_records;"
psql "$DATABASE_URL" -c "SELECT count(*) FROM daily_cost_summaries;"
psql "$DATABASE_URL" -c "SELECT count(*) FROM cost_anomalies;"
curl "http://localhost:8080/api/v1/dashboard/overview?provider=oci"
curl "http://localhost:8080/api/v1/dashboard/cost-timeseries?window=daily&provider=oci&from=$(date -u -d '30 days ago' +%F)&to=$(date -u +%F)"
curl "http://localhost:8080/api/v1/dashboard/cost-by-region?provider=oci&from=$(date -u -d '30 days ago' +%F)&to=$(date -u +%F)"
curl "http://localhost:8080/api/v1/dashboard/cost-by-compartment?provider=oci&region=ap-mumbai-1&from=$(date -u -d '30 days ago' +%F)&to=$(date -u +%F)&limit=15"
curl "http://localhost:8080/api/v1/dashboard/cost-by-service?provider=oci&region=ap-mumbai-1&compartment=production&from=$(date -u -d '30 days ago' +%F)&to=$(date -u +%F)&limit=15"
curl "http://localhost:8080/api/v1/dashboard/oci-cost-drivers?region=ap-mumbai-1&compartment=production&from=$(date -u -d '30 days ago' +%F)&to=$(date -u +%F)&limit=15"
curl "http://localhost:8080/api/v1/dashboard/anomalies?provider=oci&from=$(date -u -d '30 days ago' +%F)&to=$(date -u +%F)"
curl "http://localhost:8080/api/v1/dashboard/ingestion-status"
```

`POST /api/v1/ingest` returns immediately with a background job. This keeps API clients from timing out while large OCI reports are streamed and inserted. If an ingestion is already running, Torvix returns the active job instead of starting a duplicate run. Recent job status is retained in memory for follow-up checks.

```json
{
  "job_id": "1760000000000000000",
  "status": "queued",
  "message": "ingestion queued and running in the background",
  "status_url": "/api/v1/ingest/status/1760000000000000000",
  "queued_at": "2026-05-18T03:17:19Z"
}
```

When enabled alerting targets are configured, Torvix sends an ingestion completion notification with success, partial failure, or failure status plus files and record counts. Cost reports are delivered by the report scheduler, not by every ingestion run, so a short ingestion interval cannot send repeated daily reports.

Ingestion status separates parsing from retained inserts. `records_parsed` is the number of billing rows read from downloaded reports, `records_within_lookback` is the number of rows whose usage timestamp is inside the configured lookback window, `records_skipped_old` is the number of historical rows skipped before storage, and `records_inserted` is the number of records actually handed to PostgreSQL. If an OCI report contains only historical data, a successful job can report:

```json
{
  "records_parsed": 5911,
  "records_within_lookback": 0,
  "records_skipped_old": 5911,
  "records_inserted": 0
}
```

After new records are inserted, Torvix refreshes the affected daily, weekly, and monthly dashboard summary windows, recomputes anomalies for the affected daily window, recomputes a 7-day trailing-average forecast, prunes dashboard tables outside the 90-day horizon, and then serves Grafana from those precomputed tables.

## Waste Detection

Torvix Phase 1 waste detection is OCI-only and scans the configured OCI region. It syncs complete successful inventory runs into provider-neutral resource and relationship tables, correlates active/current resources with recent cost records, and creates recommendation-only findings for possible waste. Torvix does not delete, stop, resize, retag, or modify resources.

Supported OCI Phase 1 rules:

- `OCI_DETACHED_BLOCK_VOLUME`
- `OCI_DETACHED_BOOT_VOLUME`
- `OCI_STOPPED_COMPUTE_WITH_PAID_STORAGE`
- `OCI_UNUSED_RESERVED_PUBLIC_IP`

AWS cost ingestion remains available through CUR/S3 or Cost Explorer, but AWS waste detection is planned for Phase 2 because it needs live AWS inventory and utilization APIs.

Waste detection defaults:

```bash
TORVIX_WASTE_DETECTION_ENABLED=true
TORVIX_WASTE_PROVIDER=oci
TORVIX_WASTE_SCAN_INTERVAL_HOURS=24
TORVIX_WASTE_MIN_RESOURCE_AGE_DAYS=7
TORVIX_WASTE_STOPPED_INSTANCE_MIN_DAYS=3
TORVIX_WASTE_MIN_COST_THRESHOLD=0
TORVIX_WASTE_HIGH_MONTHLY_THRESHOLD=50
TORVIX_WASTE_ENABLE_TAG_EXCLUSIONS=true
```

Main APIs:

```bash
curl "http://localhost:8080/api/v1/waste/summary?provider=oci"
curl "http://localhost:8080/api/v1/waste/findings?provider=oci&status=open"
curl "http://localhost:8080/api/v1/waste/rules"
```

The waste summary API is an open-findings summary. Grafana can use the Torvix API datasource for open finding count, estimated monthly waste, waste by severity/service/region/scope, and a top findings table. See [docs/waste-detection.md](docs/waste-detection.md) for required OCI permissions, exclusion tags, status updates, and panel suggestions.

## AWS Ingestion

AWS defaults to CUR 2.0 / Data Export files in S3 so it follows the same architecture as OCI billing exports: bucket export -> Torvix collector -> normalized PostgreSQL records -> dashboards and reports. Cost Explorer remains available as an optional `cost_explorer` mode for quick testing, debugging, or manual fallback.

AWS intentionally uses Linked Account as the billing scope instead of VPC:

- OCI drilldown remains Region -> Compartment -> Service.
- AWS v1 drilldown is Region -> Linked Account -> Service.
- Future AWS drilldown can extend to Region -> Linked Account -> Project -> VPC -> Service -> Resource.

VPC/project-level attribution is planned for a future CUR-based pass using tags, cost categories, inventory enrichment, and optional VPC-to-project mappings. Torvix does not assume every AWS charge can map to a VPC; S3, Route 53, CloudFront, IAM, Support, global data transfer, and other account/global services may need tag, cost-category, account-level, manual, or unallocated attribution.

### AWS CUR/S3 Setup

Create an S3 bucket for AWS billing exports, create an AWS Data Export / CUR 2.0 export, choose CSV gzip, set an export prefix, and wait for AWS to write the first report. Torvix currently supports `csv` and `csv_gzip`; Parquet support is planned.

Use an IAM principal with read-only access to the billing export bucket and prefix:

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

Enable CUR/S3 mode in `configs/config.yaml` or environment variables:

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

For local contributor testing without AWS access or Cost Explorer API calls, point Torvix at a sanitized local CUR CSV or gzip file:

```bash
TORVIX_AWS_ENABLED=true
TORVIX_AWS_INGESTION_MODE=cur_s3
TORVIX_AWS_CUR_LOCAL_PATH=./testdata/aws/cur-sample.csv.gz
```

CUR records are stored with `provider='aws'`, `billing_scope_type='linked_account'`, and `record_type='cur_line_item'`. Re-reading the same export rows uses a deterministic `source_record_hash`, so ingestion updates existing rows instead of duplicating them. General totals, reports, anomalies, and forecasts aggregate canonical CUR line items.

With defaults, an ingestion on `2026-06-01` reprocesses recent billing exports covering `2026-05-29` through `2026-05-31`; AWS daily reports use a 2-day lag, so the stable report date is `2026-05-30`.

### Optional Cost Explorer Mode

Cost Explorer is not the default ingestion architecture. Enable it explicitly only when you want quick testing or a manual fallback:

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

Torvix calls Cost Explorer with daily granularity for total cost, service, linked account, region, linked account + service, and region + service. Cost Explorer allows at most two `GroupBy` dimensions per request, so Region + Linked Account + Service is not requested. If AWS rejects region grouping, Torvix logs a warning and continues with the other queries. Cost Explorer records use `linked_account_service` for general totals and `region_service` for region views to avoid double-counting overlapping query outputs.

Validate AWS data after ingestion:

```bash
curl -X POST http://localhost:8080/api/v1/ingest
curl "http://localhost:8080/api/v1/dashboard/overview?provider=aws"
curl "http://localhost:8080/api/v1/dashboard/cost-by-region?provider=aws&from=$(date -u -d '30 days ago' +%F)&to=$(date -u +%F)"
curl "http://localhost:8080/api/v1/dashboard/cost-by-scope?provider=aws&from=$(date -u -d '30 days ago' +%F)&to=$(date -u +%F)"
curl "http://localhost:8080/api/v1/dashboard/drilldown?provider=aws&from=$(date -u -d '30 days ago' +%F)&to=$(date -u +%F)"
```

## Anomaly Detection

Torvix does not use AI/ML for anomaly detection today. The v1 anomaly model is deterministic and explainable:

- It evaluates daily precomputed spend by provider, account, service, category, and region.
- It compares each day against the trailing 7-day baseline within the retained 90-day horizon.
- It stores observed cost, expected cost, absolute delta, percentage delta, severity, method, and an explanation in `cost_anomalies`.
- A row is flagged when the absolute delta is at least `1.00` and either percentage deviation is at least `30%` or z-score is at least `2`.
- Severity is `high` at `50%` deviation or z-score `3`; otherwise matching rows are `medium`.

Example explanation:

```text
OCI Object Storage daily spend was 82.0% above its trailing baseline: observed 18.40, expected 10.11.
```

This is intentionally debuggable operational statistics, not predictive ML. Tune thresholds in code only after validating false positives against real billing history.

## API

- `GET /healthz`
- `POST /api/v1/ingest`
- `GET /api/v1/ingest/status/{job_id}`
- `GET /api/v1/analytics/summary?from=YYYY-MM-DD&to=YYYY-MM-DD&window=daily|weekly|monthly`
- `GET /api/v1/analytics/anomalies?from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/v1/analytics/forecast?from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/v1/dashboard/overview?provider=oci|aws`
- `GET /api/v1/dashboard/cost-timeseries?provider=oci|aws&from=YYYY-MM-DD&to=YYYY-MM-DD&window=daily|weekly|monthly`
- `GET /api/v1/dashboard/cost-by-category?provider=oci|aws&from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/v1/dashboard/cost-by-service?provider=oci|aws&region=<region>&scope=<scope>&service=<service>&from=YYYY-MM-DD&to=YYYY-MM-DD&limit=15`
- `GET /api/v1/dashboard/cost-by-provider?provider=oci|aws&from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/v1/dashboard/cost-by-compartment?provider=oci&region=<region>&from=YYYY-MM-DD&to=YYYY-MM-DD&limit=15`
- `GET /api/v1/dashboard/cost-by-scope?provider=oci|aws&region=<region>&from=YYYY-MM-DD&to=YYYY-MM-DD&limit=15`
- `GET /api/v1/dashboard/cost-by-region?provider=oci|aws&from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/v1/dashboard/drilldown?provider=oci|aws&from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/v1/dashboard/oci-cost-summary?region=<region>&compartment=<compartment>&service=<service>&from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/v1/dashboard/oci-cost-drivers?region=<region>&compartment=<compartment>&service=<service>&from=YYYY-MM-DD&to=YYYY-MM-DD&limit=15`
- `GET /api/v1/dashboard/anomalies?provider=oci&from=YYYY-MM-DD&to=YYYY-MM-DD&severity=high`
- `GET /api/v1/dashboard/ingestion-status`
- `GET /api/v1/reports/daily?from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/v1/reports/weekly?from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/v1/reports/monthly?from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /metrics`

When `api.auth.enabled` is true, every endpoint above except `/healthz` and `/swagger/*` requires `Authorization: Bearer <token>`. Use the same token in Grafana Infinity, Superset, or any custom client.

The bundled OCI Grafana dashboard drills into cost in this order: Region -> Compartment -> Service. The Region, Compartment, and Service variables support `All`; when a variable is `All`, the matching API filter is not applied. The dashboard uses the selected Grafana time range and shows aggregate/top values until a Region, Compartment, or Service filter is selected. `Top OCI Cost Drivers` returns Region, Compartment, Service, Total Cost, and percent of the filtered total.

Report endpoints use operational FinOps windows by default:

- Daily: day-1 in the configured report timezone. With the default `Asia/Kolkata` timezone, a report generated on `2026-06-01` targets `2026-05-31`.
- Weekly: the previous full Monday-to-Sunday week. A report generated on Monday, `2026-06-01`, covers `2026-05-25` through `2026-05-31`.
- Monthly: the last completed calendar month.

Pass `from=YYYY-MM-DD&to=YYYY-MM-DD` to override those defaults. Add `deliver=true` to send the report to enabled alerting targets.

## Local Development

```bash
cp configs/config.example.yaml configs/config.yaml
cp .env.example .env
make tidy
make test
make compose-dev-up
curl -X POST http://localhost:8080/api/v1/ingest
curl "http://localhost:8080/api/v1/analytics/summary?window=weekly&from=2026-05-01&to=2026-05-31"
```

The Compose files load `.env` into the Torvix container with `env_file`; update it for AWS credentials, AWS CUR settings, report scheduler overrides, and port/resource limits. The application applies SQL migrations from `migrations/` on startup.

Torvix has two Docker Compose entry points:

- `docker-compose.dev.yml`: full local stack with Torvix, PostgreSQL/TimescaleDB, Prometheus, and Grafana.
- `docker-compose.prod.yml`: Torvix app only for production environments that already have PostgreSQL/TimescaleDB, Prometheus, and Grafana.

- **API:** `http://localhost:8080`
- **Swagger UI:** `http://localhost:8080/swagger/index.html`
- **Grafana:** `http://localhost:3000` (Torvix API, Prometheus, and local PostgreSQL datasources are automatically provisioned; the dev container installs the Infinity datasource with `GF_PLUGINS_PREINSTALL_SYNC`)
- **Prometheus:** `http://localhost:9090`

The bundled Grafana dashboards read from the Torvix API and Prometheus. The local PostgreSQL datasource is only for direct development inspection; production Grafana should keep PostgreSQL private.

Torvix listens on `http.address` from config by default. Override the actual app listener at runtime with `TORVIX_HTTP_ADDRESS`, `TORVIX_HTTP_PORT`, or `TORVIX_API_PORT`, which is useful when using host networking.

Torvix writes JSON logs to subsystem files instead of stdout. Compose mounts `./logs` to `/app/logs`; control logging with `TORVIX_LOG_LEVEL`, `TORVIX_LOG_DIR`, and `TORVIX_LOG_RETENTION_DAYS`.

For production setup, Prometheus scraping, and Grafana dashboard import instructions, see `docs/deployment.md`. If you change the app port, update the Prometheus scrape target in `deploy/prometheus.yml` or your production Prometheus config to match.

## Configuration Highlights

In `configs/config.yaml`:

- **Ingestion lifecycle:** Torvix ingests daily billing exports with a 30-day lookback and retains operational analytics for 90 days by default.
  ```yaml
  ingestion:
    lookback_days: 30
    retention_days: 90
    compression_after_days: 7
  ```
  Object-level report selection and dedupe reduce unnecessary downloads. For OCI proprietary cost reports, Torvix uses `reports/cost-csv/` candidates, seeks near the recent metadata window, sorts the bounded candidate set newest-first, skips already processed reports, and stops after `max_zero_yield_files` consecutive processed reports contain zero rows inside the lookback window. Record-level lookback filtering is the correctness boundary for dashboard data: records older than `lookback_days` are skipped before insertion. Retention remains a storage lifecycle safety net, not the primary ingestion lookback filter.
- **Logging:** Torvix writes file-only JSON logs split by subsystem.
  ```yaml
  logging:
    level: info
    dir: logs
    retention_days: 14
  ```
  The files are `app.log`, `http.log`, `ingestion.log`, `db.log`, `oci.log`, `aws.log`, `scheduler.log`, `alerting.log`, and `waste.log`. Files older than `retention_days` are deleted from the configured log directory.
- **Scheduler:** Torvix includes an in-process scheduler to run ingestion automatically.
  ```yaml
  scheduler:
    enabled: true
    ingest_interval: "24h"
  ```
  If `ingest_interval` is omitted, Torvix defaults to `24h`.
- **Alerting:** Set up Slack, Microsoft Teams, Telegram, Discord, or SMTP email targets to receive ingestion completion notifications and scheduled daily/weekly reports. Partial or failed ingestion sends only the ingestion completion notification. Targets are disabled by default; keep credentials in local or deployment-specific config. Reports include the top 5 anomalies plus the most significant cost increases and decreases, and leave full details in Grafana/API views.
  ```yaml
  reporting:
    timezone: "Asia/Kolkata"
    daily_report_cron: "0 14 * * *"
    weekly_report_cron: "0 15 * * 1"
    require_complete_ingestion: true
    daily_report_target_lag_days: 1
    webhooks:
      - name: slack-finops
        type: slack
        url: "https://hooks.slack.com/services/..."
        currency: INR
        enabled: false
      - name: teams-finops
        type: teams
        url: "https://outlook.office.com/webhook/..."
        currency: INR
        enabled: false
      - name: telegram-finops
        type: telegram
        bot_token: "..."
        chat_id: "..."
        currency: INR
        enabled: false
      - name: discord-finops
        type: discord
        url: "https://discord.com/api/webhooks/..."
        currency: INR
        enabled: false
      - name: email-finops
        type: email
        smtp_host: "smtp.example.com"
        smtp_port: 587
        username: "..."
        password: "..."
        from: "torvix@example.com"
        to:
          - "finops@example.com"
        subject_prefix: "[Torvix]"
        currency: INR
        enabled: false
  ```
  Defaults are 2:00 PM IST daily and 3:00 PM IST every Monday. Daily reports use day-1 data. Weekly reports cover the previous Monday-to-Sunday range. When `require_complete_ingestion` is enabled, Torvix skips daily or weekly delivery if the target date or full weekly range has no ingested daily data. Successful deliveries are recorded per provider, report type, period range, and destination so the same report is not sent repeatedly.

## Backup and Restore

- Backup: `pg_dump -Fc -h localhost -U torvix torvix > torvix.dump`
- Restore: `pg_restore -d torvix -h localhost -U torvix --clean torvix.dump`

For local TimescaleDB data resets, use `make compose-dev-down`.
