# Repository Guidelines

## Project Structure & Module Organization

`cmd/cloudpulse` contains the application entrypoint. `internal/app` wires configuration, migrations, lifecycle policies, collectors, services, HTTP routes, and scheduler startup. Core behavior lives under `internal/core` for collection, normalization, analytics, forecasting, reporting, and alerting. Shared models, config, and logging live in `internal/domain`, `internal/config`, and `internal/logging`.

Infrastructure code belongs in `internal/adapters`: PostgreSQL/TimescaleDB persistence in `internal/adapters/postgres`, Prometheus metrics in `internal/adapters/prometheus`, and cloud collectors in `internal/adapters/providers`. HTTP handlers and API response contracts are in `internal/ports/http`; provider/storage interfaces live under `internal/ports`. SQL migrations are in `migrations`. Runtime assets are in `configs`, `deploy`, `docker`, and `dashboards`. Generated API docs live in `docs`. Cross-package tests live in `tests/unit`, with focused package tests beside the code they cover.

## Current Architecture Rules

CloudPulse permanently uses PostgreSQL with the TimescaleDB extension. Do not add ClickHouse, dual database paths, or generic repository indirection unless the user explicitly asks for a new backend.

The project is operational FinOps tooling, not archival billing warehousing. Keep `ingestion.retention_days` at 90 days by default and `compression_after_days` at 7 days unless a task explicitly changes the lifecycle. Raw `cost_records`, dashboard summaries, anomalies, and forecasts should stay aligned to that operational horizon.

OCI is a first-class provider. The current runtime only wires collectors for enabled providers; if only OCI is enabled, AWS, Azure, and GCP collectors must not run. Keep provider-specific parsing, mapping, and Object Storage behavior in `internal/adapters/providers/oci`, behind the provider interfaces in `internal/ports/providers`.

Dashboard cost panels must read CloudPulse HTTP APIs backed by precomputed PostgreSQL/TimescaleDB summary tables. Production Grafana must not query PostgreSQL directly. Keep Prometheus focused on operational metrics; do not push high-cardinality billing dimensions into Prometheus labels.

## Branching & Release Workflow

Do not implement feature work directly on `main`. After a release, merge, or clean checkpoint on `main`, create a focused feature branch before editing code, using names like `feat/post-ingestion-report-alerts` or `fix/oci-parser-drift`. Keep `main` for merged, releasable states and release tags only.

Before starting implementation, run `git status -sb` and `git branch --show-current`. If the current branch is `main`, switch to a feature branch first. If feature work was accidentally started on `main`, create a feature branch immediately with the uncommitted work still present, then continue from that branch.

When the user asks to release, merge the feature branch back to `main`, tag from `main`, push `main` and the tag, then delete merged feature branches locally and remotely.

## Build, Test, and Development Commands

- `make build`: compile Go packages under `cmd` and `internal`.
- `make run`: start the API locally with `configs/config.yaml`.
- `make test`: run Go tests for `cmd`, `internal`, and `tests`.
- `make fmt`: run `go fmt ./...`.
- `make tidy`: synchronize Go module dependencies.
- `make swagger`: regenerate `docs` from Swagger annotations with `swag`.
- `make compose-dev-up`: start the full local stack: CloudPulse, TimescaleDB, Prometheus, and Grafana.
- `make compose-dev-down`: stop the local stack and remove volumes.
- `make compose-dev-config`: validate the dev Compose file.
- `make compose-prod-up`: start only the CloudPulse production app container.
- `make compose-prod-down`: stop the production app container.
- `make compose-prod-config`: validate the production Compose file.

If Go needs writable build caches in this environment, use:

```bash
env GOCACHE=/tmp/cloudpulse-go-build GOMODCACHE=/tmp/cloudpulse-go-mod go test ./...
```

## Coding Style & Naming Conventions

Use standard Go formatting and keep code `gofmt` clean. Prefer small packages with explicit responsibilities and constructor-style `New(...)` functions. Exported identifiers use `CamelCase`; unexported helpers use `camelCase`. Keep interfaces in `internal/ports` and concrete implementations in `internal/adapters`.

Use structured logging through `log/slog`, especially around provider discovery, download, parsing, normalization, insertion, dashboard refresh, retention, and query execution boundaries. Keep YAML keys `snake_case`, and preserve environment override behavior for `CLOUDPULSE_HTTP_ADDRESS`, `CLOUDPULSE_HTTP_PORT`, and `CLOUDPULSE_GRAFANA_API_BEARER_TOKEN`.

## Ingestion Contract

`POST /api/v1/ingest` is asynchronous. It should return `202` with `job_id`, `status`, `message`, `status_url`, and `queued_at`, then process ingestion in the background. `GET /api/v1/ingest/status/{job_id}` exposes queued, running, success, partial failure, or failed state with provider result details. Do not reintroduce long blocking HTTP ingestion.

Provider results should keep the explicit counters used by docs and dashboards: `files_processed`, `files_skipped`, `skipped_old_files`, `records_parsed`, `records_within_lookback`, `records_skipped_old`, `records_inserted`, and `duration_seconds`. Completion notifications should go through alerting targets when configured.

OCI ingestion must use official OCI SDK config-file auth, list and stream Object Storage cost reports, tolerate CSV header drift, handle gzip, preserve source object metadata, and use `processed_report_files` as the idempotency boundary. Store batch data and processed-file markers transactionally when possible. Record-level lookback filtering is the correctness boundary; do not rely on object timestamps or retention cleanup to hide old rows.

## Dashboard And API Guidelines

Dashboard APIs under `/api/v1/dashboard/*` should serve precomputed tables such as `daily_cost_summaries`, `weekly_cost_summaries`, `monthly_cost_summaries`, and `cost_anomalies`. After ingestion inserts records, refresh affected summaries, anomalies, forecasts, and lifecycle pruning before dashboard reads depend on them.

Provider-scoped dashboard endpoints are part of the public contract, especially:

- `/api/v1/dashboard/overview?provider=oci`
- `/api/v1/dashboard/cost-timeseries?window=daily&provider=oci`
- `/api/v1/dashboard/cost-by-category?provider=oci`
- `/api/v1/dashboard/cost-by-service?provider=oci`
- `/api/v1/dashboard/cost-by-provider?provider=oci`
- `/api/v1/dashboard/cost-by-compartment?provider=oci`
- `/api/v1/dashboard/cost-by-region?provider=oci`
- `/api/v1/dashboard/cost-increases?provider=oci`
- `/api/v1/dashboard/anomalies?provider=oci`
- `/api/v1/dashboard/ingestion-status`

When changing Grafana dashboards or provisioning, validate that the running dashboard actually calls the CloudPulse APIs on load. Curling endpoints or editing a panel manually is not enough. Inspect the live provisioned dashboard if behavior differs from JSON on disk.

## Testing Guidelines

Write table-driven Go tests where practical. Name test files `*_test.go` and test functions `TestXxx`. Add or update tests for analytics logic, dashboard summaries, retention/lifecycle SQL, normalization rules, provider parsing, ingestion job status, and Grafana API responses whenever those areas change.

Before completing backend changes, run `make test` or the explicit `go test` command above. For Compose or deployment changes, run `make compose-dev-config` and/or `make compose-prod-config`. For Swagger changes, prefer `make swagger`; if `swag` is unavailable and generated docs are patched manually, validate `docs/swagger.json` with `jq empty docs/swagger.json`.

## Commit & Pull Request Guidelines

Use concise imperative commit messages, for example `fix dashboard date range macros` or `add oci cost increase panel`. Keep commits focused and avoid mixing refactors with behavior changes. PRs should include a short summary, impacted areas, configuration or migration changes, validation commands, and example API calls or screenshots when dashboards change.

## Security & Configuration Tips

Do not commit real cloud credentials, database passwords, webhook URLs, SMTP credentials, billing exports, or OCI config files. Start from `configs/config.example.yaml` or `configs/config.prod.example.yaml` and keep real environment files ignored.

Treat cloud collectors and billing exports as external-input boundaries: validate file formats, sanitize parsed fields, tolerate schema drift, and preserve bucket, object name, ETag, and provider metadata for traceability. In production, keep PostgreSQL private to CloudPulse and database administration paths; Grafana should access cost data only through CloudPulse API endpoints and Prometheus.
