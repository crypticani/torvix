# Repository Guidelines

## Project Structure & Module Organization

`cmd/cloudpulse` contains the application entrypoint. Core business logic lives under `internal/core` with separate packages for collection, normalization, analytics, forecasting, reporting, and alerting. Shared models and configuration are in `internal/domain`, `internal/config`, and `internal/logging`. Infrastructure adapters live in `internal/adapters` for PostgreSQL/TimescaleDB, Prometheus, and cloud providers. HTTP handlers are in `internal/ports/http`, schema migrations are in `migrations`, and deployment assets are under `deploy`, `docker`, `dashboards`, and `configs`. Unit tests live in `tests/unit`.

## Build, Test, and Development Commands

- `make build`: compile the Go services.
- `make run`: start the API locally with `configs/config.example.yaml`.
- `make test`: run all Go tests.
- `make fmt`: format the codebase with `go fmt`.
- `make tidy`: clean and synchronize module dependencies.
- `make compose-up`: launch PostgreSQL + TimescaleDB, CloudPulse, Prometheus, and Grafana via Docker Compose.
- `make compose-down`: stop the local stack and remove volumes.

## Coding Style & Naming Conventions

Use standard Go formatting and keep code `gofmt`-clean. Prefer small packages with explicit responsibilities and constructor-style `New(...)` functions. Exported identifiers use `CamelCase`; unexported helpers use `camelCase`. Keep interfaces in `internal/ports` and concrete implementations in `internal/adapters`. Use structured logging through `log/slog` and keep configuration YAML keys `snake_case`.

## Testing Guidelines

Write table-driven Go tests where practical. Name test files `*_test.go` and test functions `TestXxx`. Keep unit tests close to the behavior they validate or under `tests/unit` for cross-package checks. Run `make test` before opening a PR. Add tests for analytics logic, normalization rules, and provider parsing whenever those modules change.

## Commit & Pull Request Guidelines

No Git history is available in this workspace, so use concise imperative commit messages such as `add oci billing parser` or `fix anomaly aggregation window`. Keep commits focused and avoid mixing refactors with behavior changes. PRs should include a short summary, impacted areas, configuration or migration changes, and example API calls or screenshots when dashboards change.

## Security & Configuration Tips

Do not commit real cloud credentials, webhook URLs, or billing exports. Start from `configs/config.example.yaml` and inject secrets via environment-specific files or deployment tooling. Treat OCI, AWS, Azure, and GCP collectors as external-input boundaries: validate file formats, sanitize parsed fields, and preserve source object metadata for traceability.
