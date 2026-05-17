# CloudPulse

CloudPulse is a self-hosted multi-cloud FinOps analytics platform for ingesting billing exports, normalizing cost data, detecting anomalies, forecasting spend, generating reports, and exposing Prometheus-compatible metrics.

CloudPulse now uses PostgreSQL with the TimescaleDB extension as its permanent and only database backend.

## Capabilities

- Daily billing export ingestion
- Canonical multi-cloud normalization
- Daily, weekly, and monthly Timescale continuous aggregates
- Anomaly detection using moving averages, z-score, and percentage deviation
- Rolling forecast generation
- Slack and Discord webhook delivery
- Prometheus metrics and Grafana dashboards
- Self-hosted Docker Compose deployment

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
- continuous aggregates power daily, weekly, and monthly rollups
- compression and retention policies are defined in migrations
- `processed_report_files` tracks incremental billing ingestion and idempotency

## OCI Billing Ingestion

OCI support is production-oriented and first-class. The collector:

- authenticates with the official OCI Go SDK using an OCI config file and API keys
- lists report objects from OCI Object Storage
- streams CSV and gzip-compressed exports
- tolerates schema drift through dynamic header matching
- normalizes OCI services into Compute, Storage, Networking, Database, Load Balancer, Monitoring, Security, and Kubernetes categories
- skips files already recorded in `processed_report_files`

Oracle deprecated older usage reports on January 31, 2025, so the parser accepts both older usage-style headers and current OCI cost report layouts.

### OCI Setup

1. Create an OCI API key for a user with permission to read usage report objects.
2. Mount an OCI config file into the container, for example `/app/configs/oci_config`.
3. Set the OCI provider in `configs/config.yaml`:

```yaml
providers:
  aws:
    enabled: false
  azure:
    enabled: false
  gcp:
    enabled: false
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
psql "$DATABASE_URL" -c "SELECT count(*) FROM cost_records;"
curl "http://localhost:8080/api/v1/analytics/summary?window=weekly"
```

If only OCI is enabled, `/api/v1/ingest` returns one object:

```json
{
  "provider": "oci",
  "files_processed": 1,
  "records_parsed": 100,
  "records_inserted": 100,
  "duration_seconds": 2.4
}
```

## API

- `GET /healthz`
- `POST /api/v1/ingest`
- `GET /api/v1/analytics/summary?from=YYYY-MM-DD&to=YYYY-MM-DD&window=daily|weekly|monthly`
- `GET /api/v1/analytics/anomalies?from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/v1/analytics/forecast?from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/v1/reports/daily?from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/v1/reports/weekly?from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/v1/reports/monthly?from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /metrics`

Report endpoints use operational FinOps windows by default:

- Daily: yesterday.
- Weekly: the last completed Monday-to-Monday week.
- Monthly: the last completed calendar month.

Pass `from=YYYY-MM-DD&to=YYYY-MM-DD` to override those defaults. Add `deliver=true` to send the report to enabled alerting targets.

## Local Development

```bash
make tidy
make test
make compose-up
curl -X POST http://localhost:8080/api/v1/ingest
curl "http://localhost:8080/api/v1/analytics/summary?window=weekly&from=2026-05-01&to=2026-05-31"
```

The application applies SQL migrations from `migrations/` on startup.

- **API:** `http://localhost:8080`
- **Swagger UI:** `http://localhost:8080/swagger/index.html`
- **Grafana:** `http://localhost:3000` (PostgreSQL and Prometheus datasources are automatically provisioned)
- **Prometheus:** `http://localhost:9090`

## Configuration Highlights

In `configs/config.yaml`:

- **Scheduler:** CloudPulse includes an in-process scheduler to run ingestion automatically.
  ```yaml
  scheduler:
    enabled: true
    ingest_interval: "6h"
  ```
- **Alerting:** Set up Slack, Microsoft Teams, Telegram, Discord, or SMTP email targets to receive daily/weekly/monthly cost reports. Targets are disabled by default; keep credentials in local or deployment-specific config. Notifications include the top 5 anomalies and leave the full anomaly list in Grafana/API views.
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

For local TimescaleDB data resets, use `make compose-down`.
