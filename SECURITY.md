# Security Policy

## Supported Versions

Security fixes are handled on the current released version line. Users should upgrade to the latest published Torvix image or release tag when a security fix is announced.

## Reporting a Vulnerability

Do not open a public issue for secrets exposure, authentication bypass, cloud credential handling, database access, or billing-data disclosure vulnerabilities.

Report privately by emailing the maintainer listed on the GitHub repository profile, or by using GitHub private vulnerability reporting if it is enabled for the repository.

Please include:

- Affected version or commit.
- Deployment mode, such as Docker Compose or a custom container deployment.
- Impacted surface, such as API auth, Grafana datasource access, cloud provider credentials, migrations, or billing data.
- Reproduction steps with sanitized data only.

Do not include real AWS keys, OCI private keys, API bearer tokens, webhook URLs, SMTP credentials, database credentials, billing exports, or customer identifiers.

## Security Expectations

- Keep Torvix API authentication enabled for public deployments.
- Keep PostgreSQL/TimescaleDB private to Torvix and administrative access paths.
- Configure Grafana, Superset, and custom clients to use the Torvix API bearer token instead of direct database access.
- Mount config and credential files read-only, owned by the host user, and group-readable by the Torvix container group.
