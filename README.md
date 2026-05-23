# CloudPulse

CloudPulse is a self-hosted multi-cloud FinOps analytics platform for ingesting daily billing exports, normalizing cost data, detecting anomalies, forecasting spend, generating reports, and exposing Prometheus-compatible metrics.

CloudPulse now uses PostgreSQL with the TimescaleDB extension as its permanent and only database backend.
It is operational FinOps tooling, not long-term archival billing warehousing; the supported default historical horizon is 90 days.

## Capabilities

- Daily billing export ingestion
- Canonical multi-cloud normalization
- Daily, weekly, and monthly precomputed dashboard summaries
- Explainable anomaly detection using trailing baselines, percentage deviation, and optional z-score thresholds
- Rolling forecast generation
- Slack, Microsoft Teams, Telegram, Discord, and SMTP email report delivery
- Prometheus metrics and Grafana dashboards
- Separate Docker Compose files for full local development and production app-only deployment

## Architecture

```text
cmd/cloudpulse               application entrypoint
internal/app                 bootstrap, DB wiring, migrations
internal/adapters/postgres   pgx repository and migration runner
internal/adapters/providers  cloud collectors, including OCI Object Storage
internal/core                collection, normalization, analytics, reporting
internal/ports               collector and repository contracts
migrations                   PostgreSQL + Timescale SQL migrations
configs                      YAML configuration
deploy                       Prometheus and cron examples
dashboards                   sample Grafana dashboard JSON
```

## Storage Design

- `cost_records` is a Timescale hypertable partitioned on `timestamp`
- tags and provider metadata are stored in `JSONB`
- `daily_cost_summaries`, `weekly_cost_summaries`, `monthly_cost_summaries`, `cost_anomalies`, and `cost_forecasts` are precomputed after ingestion for dashboard APIs
- compression and retention policies are defined in migrations; raw records are retained for 90 days by default and compressed after 7 days
- `processed_report_files` tracks incremental billing ingestion and idempotency

Production dashboard flow:

```text
Grafana Infinity datasource -> CloudPulse dashboard APIs -> precomputed PostgreSQL/TimescaleDB analytics tables
Grafana Prometheus datasource -> Prometheus -> CloudPulse operational metrics
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

Object selection is only an efficiency heuristic for reaching usable reports quickly when the bucket contains many historical files. For broad OCI prefixes such as `reports/`, CloudPulse narrows selection to `reports/cost-csv/`, uses metadata to seek near the configured lookback window, and processes the bounded candidate set newest-first. CloudPulse does not treat the auto-incrementing numeric report suffix as proof that a row belongs in the dashboard. Row-level lookback remains the source of truth for deciding which billing records are inserted, and `max_zero_yield_files` prevents long no-op runs through historical reports.

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
curl "http://localhost:8080/api/v1/dashboard/cost-by-category?provider=oci&from=$(date -u -d '30 days ago' +%F)&to=$(date -u +%F)"
curl "http://localhost:8080/api/v1/dashboard/cost-by-service?provider=oci&from=$(date -u -d '30 days ago' +%F)&to=$(date -u +%F)&limit=15"
curl "http://localhost:8080/api/v1/dashboard/cost-by-compartment?provider=oci&from=$(date -u -d '30 days ago' +%F)&to=$(date -u +%F)&limit=15"
curl "http://localhost:8080/api/v1/dashboard/cost-by-region?provider=oci&from=$(date -u -d '30 days ago' +%F)&to=$(date -u +%F)"
curl "http://localhost:8080/api/v1/dashboard/anomalies?provider=oci&from=$(date -u -d '30 days ago' +%F)&to=$(date -u +%F)"
curl "http://localhost:8080/api/v1/dashboard/ingestion-status"
```

`POST /api/v1/ingest` returns immediately with a background job. This keeps API clients from timing out while large OCI reports are streamed and inserted. If an ingestion is already running, CloudPulse returns the active job instead of starting a duplicate run. Recent job status is retained in memory for follow-up checks.

```json
{
  "job_id": "1760000000000000000",
  "status": "queued",
  "message": "ingestion queued and running in the background",
  "status_url": "/api/v1/ingest/status/1760000000000000000",
  "queued_at": "2026-05-18T03:17:19Z"
}
```

When enabled alerting targets are configured, CloudPulse sends an ingestion completion notification with success, partial failure, or failure status plus files and record counts. Only successful ingestion runs deliver the daily, weekly, and monthly cost reports to the same enabled alerting targets, so reports are sent after the latest ingestion has completed cleanly.

Ingestion status separates parsing from retained inserts. `records_parsed` is the number of billing rows read from downloaded reports, `records_within_lookback` is the number of rows whose usage timestamp is inside the configured lookback window, `records_skipped_old` is the number of historical rows skipped before storage, and `records_inserted` is the number of records actually handed to PostgreSQL. If an OCI report contains only historical data, a successful job can report:

```json
{
  "records_parsed": 5911,
  "records_within_lookback": 0,
  "records_skipped_old": 5911,
  "records_inserted": 0
}
```

After new records are inserted, CloudPulse refreshes the affected daily, weekly, and monthly dashboard summary windows, recomputes anomalies for the affected daily window, recomputes a 7-day trailing-average forecast, prunes dashboard tables outside the 90-day horizon, and then serves Grafana from those precomputed tables.

## Anomaly Detection

CloudPulse does not use AI/ML for anomaly detection today. The v1 anomaly model is deterministic and explainable:

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
- `GET /api/v1/dashboard/overview?provider=oci`
- `GET /api/v1/dashboard/cost-timeseries?provider=oci&from=YYYY-MM-DD&to=YYYY-MM-DD&window=daily|weekly|monthly`
- `GET /api/v1/dashboard/cost-by-category?provider=oci&from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/v1/dashboard/cost-by-service?provider=oci&from=YYYY-MM-DD&to=YYYY-MM-DD&limit=15`
- `GET /api/v1/dashboard/cost-by-provider?provider=oci&from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/v1/dashboard/cost-by-compartment?provider=oci&from=YYYY-MM-DD&to=YYYY-MM-DD&limit=15`
- `GET /api/v1/dashboard/cost-by-region?provider=oci&from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/v1/dashboard/anomalies?provider=oci&from=YYYY-MM-DD&to=YYYY-MM-DD&severity=high`
- `GET /api/v1/dashboard/ingestion-status`
- `GET /api/v1/reports/daily?from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/v1/reports/weekly?from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/v1/reports/monthly?from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /metrics`

Report endpoints use operational FinOps windows by default:

- Daily: yesterday. If yesterday has no ingested rows yet, CloudPulse falls back to the latest prior day with data within the last 7 days so OCI billing export lag does not produce empty daily alerts.
- Weekly: the last completed Monday-to-Monday week.
- Monthly: the last completed calendar month.

Pass `from=YYYY-MM-DD&to=YYYY-MM-DD` to override those defaults. Add `deliver=true` to send the report to enabled alerting targets.

## Local Development

```bash
make tidy
make test
make compose-dev-up
curl -X POST http://localhost:8080/api/v1/ingest
curl "http://localhost:8080/api/v1/analytics/summary?window=weekly&from=2026-05-01&to=2026-05-31"
```

The application applies SQL migrations from `migrations/` on startup.

CloudPulse has two Docker Compose entry points:

- `docker-compose.dev.yml`: full local stack with CloudPulse, PostgreSQL/TimescaleDB, Prometheus, and Grafana.
- `docker-compose.prod.yml`: CloudPulse app only for production environments that already have PostgreSQL/TimescaleDB, Prometheus, and Grafana.

- **API:** `http://localhost:8080`
- **Swagger UI:** `http://localhost:8080/swagger/index.html`
- **Grafana:** `http://localhost:3000` (CloudPulse API, Prometheus, and local PostgreSQL datasources are automatically provisioned; the dev container installs the Infinity datasource with `GF_PLUGINS_PREINSTALL_SYNC`)
- **Prometheus:** `http://localhost:9090`

The bundled Grafana dashboard reads from the CloudPulse API and Prometheus. The local PostgreSQL datasource is only for direct development inspection; production Grafana should keep PostgreSQL private.

CloudPulse listens on `http.address` from config by default. Override the actual app listener at runtime with `CLOUDPULSE_HTTP_ADDRESS` or `CLOUDPULSE_HTTP_PORT`, which is useful when using host networking.

For production setup, Prometheus scraping, and Grafana dashboard import instructions, see `docs/deployment.md`. If you change the app port, update the Prometheus scrape target in `deploy/prometheus.yml` or your production Prometheus config to match.

## Configuration Highlights

In `configs/config.yaml`:

- **Ingestion lifecycle:** CloudPulse ingests daily billing exports with a 30-day lookback and retains operational analytics for 90 days by default.
  ```yaml
  ingestion:
    lookback_days: 30
    retention_days: 90
    compression_after_days: 7
  ```
  Object-level report selection and dedupe reduce unnecessary downloads. For OCI proprietary cost reports, CloudPulse uses `reports/cost-csv/` candidates, seeks near the recent metadata window, sorts the bounded candidate set newest-first, skips already processed reports, and stops after `max_zero_yield_files` consecutive processed reports contain zero rows inside the lookback window. Record-level lookback filtering is the correctness boundary for dashboard data: records older than `lookback_days` are skipped before insertion. Retention remains a storage lifecycle safety net, not the primary ingestion lookback filter.
- **Scheduler:** CloudPulse includes an in-process scheduler to run ingestion automatically.
  ```yaml
  scheduler:
    enabled: true
    ingest_interval: "24h"
  ```
  If `ingest_interval` is omitted, CloudPulse defaults to `24h`.
- **Alerting:** Set up Slack, Microsoft Teams, Telegram, Discord, or SMTP email targets to receive ingestion completion notifications and daily/weekly/monthly cost reports after successful ingestion runs. Partial or failed ingestion sends only the ingestion completion notification, not cost reports. Targets are disabled by default; keep credentials in local or deployment-specific config. Notifications include the top 5 anomalies and leave the full anomaly list in Grafana/API views.
  ```yaml
  reporting:
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
        from: "cloudpulse@example.com"
        to:
          - "finops@example.com"
        subject_prefix: "[CloudPulse]"
        currency: INR
        enabled: false
  ```

## Backup and Restore

- Backup: `pg_dump -Fc -h localhost -U cloudpulse cloudpulse > cloudpulse.dump`
- Restore: `pg_restore -d cloudpulse -h localhost -U cloudpulse --clean cloudpulse.dump`

For local TimescaleDB data resets, use `make compose-dev-down`.
