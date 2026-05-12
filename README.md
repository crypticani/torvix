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

## Local Development

```bash
make tidy
make test
make compose-up
curl -X POST http://localhost:8080/api/v1/ingest
curl "http://localhost:8080/api/v1/analytics/summary?window=weekly&from=2026-05-01&to=2026-05-31"
```

The application applies SQL migrations from `migrations/` on startup.

## Backup and Restore

- Backup: `pg_dump -Fc -h localhost -U cloudpulse cloudpulse > cloudpulse.dump`
- Restore: `pg_restore -d cloudpulse -h localhost -U cloudpulse --clean cloudpulse.dump`

For local TimescaleDB data resets, use `make compose-down`.
