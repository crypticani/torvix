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
- PostgreSQL datasource UID: `PostgreSQL`

Those UIDs match `dashboards/cloudpulse-overview.json`.

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

4. Start only the CloudPulse app:

   ```bash
   docker compose -f docker-compose.prod.yml up --build -d
   ```

   The Makefile alias is:

   ```bash
   make compose-prod-up
   ```

5. Validate the app:

   ```bash
   curl http://localhost:8080/healthz
   curl http://localhost:8080/metrics
   ```

The production Compose file exposes CloudPulse on host port `8080` by default. Override it with:

```bash
CLOUDPULSE_HTTP_PORT=18080 docker compose -f docker-compose.prod.yml up --build -d
```

Resource limits can be tuned with:

```bash
CLOUDPULSE_CPU_LIMIT=1.0 CLOUDPULSE_MEMORY_LIMIT=512M docker compose -f docker-compose.prod.yml up --build -d
```

## Connect Production Prometheus

CloudPulse exposes Prometheus metrics at:

```text
http://<cloudpulse-host>:8080/metrics
```

Add this scrape job to your existing Prometheus configuration:

```yaml
scrape_configs:
  - job_name: cloudpulse
    metrics_path: /metrics
    scrape_interval: 15s
    static_configs:
      - targets:
          - cloudpulse.example.internal:8080
```

An example snippet is available at `deploy/prometheus.prod-scrape.example.yml`.

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
cloudpulse_records_pruned_total
cloudpulse_compressed_chunks_total
```

## Import Dashboard Into Production Grafana

The dashboard file is:

```text
dashboards/cloudpulse-overview.json
```

The dashboard expects these Grafana datasource UIDs:

- `Prometheus`: your production Prometheus datasource.
- `PostgreSQL`: your production PostgreSQL/TimescaleDB datasource.

### Option 1: Grafana UI Import

1. In Grafana, go to **Dashboards -> New -> Import**.
2. Upload or paste `dashboards/cloudpulse-overview.json`.
3. If Grafana asks for datasources, map:
   - `Prometheus` to your production Prometheus datasource.
   - `PostgreSQL` to your production PostgreSQL/TimescaleDB datasource.
4. Open the dashboard and select the relevant `Currency` variable.

If your existing datasource UIDs are different and Grafana does not prompt for mapping, either create datasource aliases with the UIDs above or edit the JSON before import.

### Option 2: Grafana Provisioning

Copy the dashboard JSON to your Grafana dashboard provisioning path and configure a provider similar to:

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

Create or update Grafana datasource provisioning so the UIDs match:

```yaml
apiVersion: 1

datasources:
  - name: Prometheus
    uid: Prometheus
    type: prometheus
    access: proxy
    url: http://prometheus.example.internal:9090
    isDefault: true

  - name: PostgreSQL
    uid: PostgreSQL
    type: postgres
    access: proxy
    url: postgres.example.internal:5432
    database: cloudpulse
    user: cloudpulse
    secureJsonData:
      password: replace_with_password
    jsonData:
      sslmode: require
      postgresVersion: 16
      timescaledb: true
```

Restart or reload Grafana provisioning after copying the files.

## Operational Notes

- Production Compose does not run PostgreSQL, Prometheus, or Grafana.
- CloudPulse applies SQL migrations on startup, so the configured database user needs permissions to create tables, indexes, Timescale hypertables, compression policies, and retention policies.
- Keep `configs/config.prod.yaml` outside git. It is ignored by `.gitignore`.
- The app should be reachable by Prometheus over the network path configured in the scrape target.
