# CloudPulse Deployment

CloudPulse has two Compose entry points:

- `docker-compose.dev.yml`: full laptop/dev stack with CloudPulse, TimescaleDB, Prometheus, and Grafana.
- `docker-compose.prod.yml`: CloudPulse application only. Use this when production already has PostgreSQL/TimescaleDB, Prometheus, and Grafana.

Do not put real OCI credentials, database passwords, alert webhooks, or SMTP passwords in tracked files. Use local ignored config files under `configs/`.

## Development Setup

The development setup is self-contained and starts all dependencies.

1. Create the local config:

   ```bash
   cp configs/config.example.yaml configs/config.yaml
   ```

2. Update `configs/config.yaml` with local OCI credentials and provider settings.

3. Start the stack:

   ```bash
   docker compose -f docker-compose.dev.yml up --build
   ```

   The Makefile alias is:

   ```bash
   make compose-dev-up
   ```

   To run CloudPulse on a different host-network port, set the app bind port:

   ```bash
   CLOUDPULSE_HTTP_PORT=18080 docker compose -f docker-compose.dev.yml up --build
   ```

   If you change the dev API port, also update `deploy/prometheus.yml` so the bundled Prometheus scrapes the same port.

4. Open the local services:

   - CloudPulse API: `http://localhost:8080`
   - Grafana: `http://localhost:3000`
   - Prometheus: `http://localhost:9090`
   - PostgreSQL/TimescaleDB: `localhost:5432`

5. Stop and remove local volumes:

   ```bash
   make compose-dev-down
   ```

The dev Grafana datasource provisioning expects:

- Prometheus datasource UID: `Prometheus`
- CloudPulse API datasource UID: `CloudPulseAPI`
- PostgreSQL datasource UID: `PostgreSQL` for local inspection only

The bundled dashboard uses `CloudPulseAPI` and `Prometheus`. It does not query PostgreSQL directly.

CloudPulse is daily operational FinOps tooling, not long-term archival billing warehousing. The default lifecycle is:

```yaml
ingestion:
  lookback_days: 30
  retention_days: 90
  compression_after_days: 7
```

Raw `cost_records` older than 90 days are removed by TimescaleDB retention and lifecycle maintenance. Precomputed dashboard summary and anomaly tables are refreshed after ingestion and pruned to the same 90-day horizon. Forecast rows are regenerated for the current forward-looking 7-day horizon and old forecast rows are pruned.

Object-level selection and `processed_report_files` dedupe reduce unnecessary OCI downloads. For broad prefixes such as `reports/`, CloudPulse narrows OCI proprietary cost report selection to `reports/cost-csv/`, seeks near the recent Object Storage metadata window, and processes the bounded candidate set newest-first. OCI numeric suffixes are not authoritative billing-period recency signals, and record-level filtering is still required because a recently modified billing export can contain historical usage rows. CloudPulse filters each parsed record by `ingestion.lookback_days` before insertion, so old rows are reported as `records_skipped_old` and never rely on retention cleanup to disappear. If selected reports produce zero rows inside the lookback window, `max_zero_yield_files` stops the run before it can spend minutes parsing historical data.

## Production Setup

Use production Compose when PostgreSQL/TimescaleDB, Prometheus, and Grafana are managed outside the CloudPulse Compose stack.

1. Create the production config:

   ```bash
   cp configs/config.prod.example.yaml configs/config.prod.yaml
   ```

2. Set the production PostgreSQL/TimescaleDB DSN:

   ```yaml
   db:
     dsn: "postgres://cloudpulse:replace_with_password@postgres.example.internal:5432/cloudpulse?sslmode=require"
   ```

   If PostgreSQL runs on the Docker host, use:

   ```yaml
   db:
     dsn: "postgres://cloudpulse:replace_with_password@host.docker.internal:5432/cloudpulse?sslmode=disable"
   ```

3. Set production OCI provider credentials in `configs/config.prod.yaml`.

4. Configure any alerting targets under `reporting.webhooks`.
   The production example includes disabled placeholders for Slack, Microsoft Teams, Telegram, Discord, and SMTP email. Keep only the targets you use, replace placeholder secrets, set the correct `currency`, and set `enabled: true`.

5. Start only the CloudPulse app:

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
CLOUDPULSE_HTTP_PORT=18080 docker compose -f docker-compose.prod.yml up --build -d
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

1. `CLOUDPULSE_HTTP_ADDRESS`, for example `0.0.0.0:18080`
2. `CLOUDPULSE_HTTP_PORT`, for example `18080`
3. `http.address` in the YAML config
4. default `:8080`

The production Compose healthcheck checks `http://127.0.0.1:${CLOUDPULSE_HTTP_PORT:-8080}/healthz`. If you use `CLOUDPULSE_HTTP_ADDRESS`, the healthcheck derives the port from that value when `CLOUDPULSE_HTTP_PORT` is not set.

If you change only `http.address` in `configs/config.prod.yaml`, also set `CLOUDPULSE_HTTP_PORT` to the same port or update the healthcheck command in `docker-compose.prod.yml`. Compose cannot read the mounted YAML value into its healthcheck automatically.

CloudPulse writes file-only JSON logs and does not emit normal application logs to stdout. Logs are split by subsystem into `app.log`, `http.log`, `ingestion.log`, `db.log`, `oci.log`, `scheduler.log`, and `alerting.log`. The bundled Compose files mount `./logs` to `/app/logs`; set `CLOUDPULSE_LOG_DIR=/app/logs` or keep `logging.dir: logs` while the container runs from `/app`.

Logging runtime controls:

```bash
CLOUDPULSE_LOG_LEVEL=debug
CLOUDPULSE_LOG_RETENTION_DAYS=14
CLOUDPULSE_LOG_DIR=/app/logs
```

CloudPulse deletes `.log` files in the configured log directory whose modification time is older than the retention window.

Resource limits can be tuned with:

```bash
CLOUDPULSE_CPU_LIMIT=1.0 CLOUDPULSE_MEMORY_LIMIT=512M docker compose -f docker-compose.prod.yml up --build -d
```

## Connect Production Prometheus

CloudPulse exposes Prometheus metrics at:

```text
http://<cloudpulse-host>:<cloudpulse-port>/metrics
```

Add this scrape job to your existing Prometheus configuration:

```yaml
scrape_configs:
  - job_name: cloudpulse
    metrics_path: /metrics
    scrape_interval: 60s
    static_configs:
      - targets:
          - cloudpulse.example.internal:8080
```

An example snippet is available at `deploy/prometheus.prod-scrape.example.yml`.

Keep the target port aligned with the CloudPulse listener. For example, if production runs with `CLOUDPULSE_HTTP_PORT=18080` or `http.address: ":18080"`, scrape `cloudpulse.example.internal:18080` instead of `cloudpulse.example.internal:8080`. The `60s` scrape interval is intentional because cost data changes on ingestion/report cadence, not every few seconds; lower it only if you need faster app health or ingestion-failure detection.

After reloading Prometheus, verify the target is up:

```promql
up{job="cloudpulse"}
```

Useful CloudPulse metrics include:

```promql
cloudpulse_processed_records_total
cloudpulse_collector_runs_total
cloudpulse_ingestion_duration_seconds_count
cloudpulse_ingestion_failures_total
cloudpulse_records_deleted_total
cloudpulse_compressed_chunks_total
```

Cost values belong in PostgreSQL-backed CloudPulse APIs, not Prometheus labels. Prometheus should carry operational health metrics only: ingestion duration, files processed, records inserted, failures, skipped old files, records pruned, compressed chunks, and API/runtime status.

If `metrics.cost_stats_enabled` is enabled, CloudPulse also exposes coarse aggregate cost gauges from dashboard summary API calls:

```promql
cloudpulse_cost_total
cloudpulse_cost_services
cloudpulse_cost_anomalies
```

These metrics only use a low-cardinality `window` label. Do not add service, account, resource, tag, source object, or raw billing dimensions as Prometheus labels.

## Import Dashboard Into Production Grafana

CloudPulse ships one OCI-specific Grafana dashboard JSON:

- `dashboards/cloudpulse-oci-finops-dashboard.json`

The file can be pasted directly into a separate production Grafana import flow after the required datasources are configured. Local development and production both use this same dashboard JSON. The local PostgreSQL datasource is provisioned only for direct developer inspection.

PostgreSQL must remain private in production. Do not expose the TimescaleDB port to Grafana users or public networks, and do not provision a production PostgreSQL datasource for dashboards. Production Grafana should read only from CloudPulse API endpoints and Prometheus.

The OCI dashboard expects these Grafana datasource UIDs:

- `Prometheus`: your production Prometheus datasource.
- `CloudPulseAPI`: an Infinity datasource that points at the CloudPulse API.

The production API endpoints are:

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

The range endpoints accept `from=YYYY-MM-DD` or RFC3339 timestamps and `to=YYYY-MM-DD` or RFC3339 timestamps. Cost time series accepts `window=daily|weekly|monthly`. Service and compartment breakdowns accept `limit=15`. The OCI dashboard uses Region -> Compartment -> Service drill-down variables; `All` means the matching filter is not applied. `Top OCI Cost Drivers` returns Region, Compartment, Service, Total Cost, and percent of the filtered total. Anomalies accepts `severity=low|medium|high`.

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

- Name: `CloudPulse API`
- UID: `CloudPulseAPI`
- Type: `yesoreyeram-infinity-datasource`
- URL: your CloudPulse API base URL, for example `https://cloudpulse.example.internal`
- Access: `Server` or `Proxy`

If CloudPulse dashboard API auth is enabled, configure the Infinity datasource to send:

```text
Authorization: Bearer <cloudpulse_grafana_api_bearer_token>
```

The bundled dashboard variables use Infinity backend JSON queries. If the datasource UID is not exactly `CloudPulseAPI`, map it during import or edit the JSON before import.

### Option 1: Grafana UI Import

1. In Grafana, go to **Dashboards -> New -> Import**.
2. Confirm the Infinity datasource plugin is installed and that a datasource with UID `CloudPulseAPI` exists.
3. Upload or paste `dashboards/cloudpulse-oci-finops-dashboard.json`.
4. If Grafana asks for datasources, map:
   - `Prometheus` to your production Prometheus datasource.
   - `CloudPulseAPI` to your CloudPulse API datasource.

If your existing datasource UIDs are different and Grafana does not prompt for mapping, either create datasource aliases with the UIDs above or edit the JSON before import.

### Option 2: Grafana Provisioning

Copy `dashboards/cloudpulse-oci-finops-dashboard.json` to your Grafana dashboard provisioning path and configure a provider similar to:

```yaml
apiVersion: 1

providers:
  - name: cloudpulse
    folder: CloudPulse
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

  - name: CloudPulse API
    uid: CloudPulseAPI
    type: yesoreyeram-infinity-datasource
    access: proxy
    url: https://cloudpulse.example.internal
    jsonData:
      auth_method: "bearerToken"
      httpHeaderName1: "Authorization"
    secureJsonData:
      httpHeaderValue1: "Bearer replace_with_cloudpulse_grafana_token"
```

Restart or reload Grafana provisioning after copying the files.

Enable the CloudPulse Grafana API auth placeholder in production config:

```yaml
grafana:
  api_auth:
    enabled: true
    bearer_token: "replace_with_long_random_token"
```

The same value can be supplied with `CLOUDPULSE_GRAFANA_API_BEARER_TOKEN`. When auth is enabled, production Grafana must send `Authorization: Bearer <token>` to `/api/v1/dashboard/*`.

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

CloudPulse anomaly detection is deterministic. It does not use AI/ML today.

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
- CloudPulse applies SQL migrations on startup, so the configured database user needs permissions to create tables, indexes, Timescale hypertables, compression policies, and retention policies.
- Keep `configs/config.prod.yaml` outside git. It is ignored by `.gitignore`.
- The app should be reachable by Prometheus over the network path configured in the scrape target.
- The database should be reachable only by CloudPulse and database administration paths, not by production Grafana.
