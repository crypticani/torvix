# CloudPulse

CloudPulse is an open-source multi-cloud FinOps analytics platform for self-hosted cost ingestion, normalization, anomaly detection, forecasting, reporting, and observability across AWS, OCI, Azure, and GCP.

## Scope

- Daily billing export ingestion
- Canonical multi-cloud schema normalization
- Daily, weekly, and monthly cost analytics
- Anomaly detection using moving averages, z-score, and percentage deviation
- Forecasting
- Slack and Discord webhook reporting
- Prometheus metrics and Grafana dashboards
- ClickHouse-backed self-hosted deployment with Docker Compose

OCI is treated as a first-class provider through a dedicated collector module and configuration surface.

## Architecture

```text
cmd/cloudpulse             application entrypoint
internal/app               bootstrap and dependency wiring
internal/config            YAML config parsing
internal/domain            core business models
internal/core/collect      ingestion orchestration
internal/core/normalize    canonical schema normalization
internal/core/analytics    aggregation and anomaly detection
internal/core/forecasting  baseline forecasting
internal/core/reporting    report generation
internal/core/alerting     webhook delivery
internal/ports             interfaces for HTTP, storage, providers
internal/adapters          ClickHouse, provider collectors, metrics
migrations                 ClickHouse schema
dashboards                 Grafana JSON dashboards
deploy                     Prometheus and cron examples
```

## API

- `GET /healthz`
- `POST /api/v1/ingest`
- `GET /api/v1/analytics/summary?from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/v1/analytics/summary?from=YYYY-MM-DD&to=YYYY-MM-DD&window=daily|weekly|monthly`
- `GET /api/v1/analytics/anomalies?from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/v1/analytics/forecast?from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/v1/reports/daily?from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/v1/reports/weekly?from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/v1/reports/monthly?from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /metrics`

## Provider Model

Collectors are isolated behind `internal/ports/providers.Collector`. Each provider adapter is responsible for:

- locating billing exports from its object storage backend
- decoding provider-specific CSV or export rows
- mapping raw rows into `domain.RawBillingRecord`

The current scaffold includes provider modules and sample data output so the full pipeline can run locally before object-storage integration is completed.

Recommended next hardening steps:

- add real object storage clients for S3, OCI Object Storage, Azure Blob, and GCP export source
- implement provider-specific parsers for CUR, OCI usage CSV, Azure export CSV, and GCP billing export
- add migration runner and scheduled jobs inside the application
- add deduplication keys and materialized aggregate tables

## Local Run

```bash
make compose-up
curl http://localhost:8080/healthz
curl -X POST http://localhost:8080/api/v1/ingest
curl "http://localhost:8080/api/v1/analytics/anomalies?from=2026-04-01&to=2026-05-12"
```

## Notes

- ClickHouse tables use `MergeTree` with month partitioning and query-oriented sort keys.
- Dependencies are intentionally minimal: YAML parsing, ClickHouse driver, Prometheus client.
- Logging uses the standard library `log/slog` JSON handler.
- The repository is structured for maintainability and testability rather than provider-specific coupling.
