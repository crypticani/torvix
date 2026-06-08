# Contributing

Torvix is production-focused FinOps software. Contributions should keep cloud cost data private, provider behavior explicit, and PostgreSQL/TimescaleDB as the single database backend.

## Development

Create a focused branch from `main` before changing code:

```bash
git switch main
git pull --ff-only
git switch -c fix/short-description
```

Run the core checks before opening a pull request:

```bash
env GOCACHE=/tmp/torvix-go-build GOMODCACHE=/tmp/torvix-go-mod go test ./...
env GOCACHE=/tmp/torvix-go-build GOMODCACHE=/tmp/torvix-go-mod go build ./cmd/... ./internal/...
make compose-dev-config
make compose-prod-config
jq empty docs/swagger.json
git diff --check
```

For migration changes that touch `cost_records`, also test against a real TimescaleDB instance:

```bash
TORVIX_TEST_DATABASE_URL='postgres://torvix:torvix@127.0.0.1:5432/torvix?sslmode=disable' \
  env GOCACHE=/tmp/torvix-go-build GOMODCACHE=/tmp/torvix-go-mod go test ./internal/adapters/postgres -count=1
```

## Pull Requests

Keep pull requests scoped. Include:

- Summary of behavior changes.
- Migration or configuration impact.
- Validation commands and relevant output.
- Screenshots or API examples for dashboard changes.

Do not include real cloud credentials, billing exports, webhook URLs, SMTP credentials, database passwords, private keys, or customer data in commits, logs, screenshots, issues, or pull requests.
