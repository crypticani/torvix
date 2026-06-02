# Torvix Waste Detection

Torvix Phase 1 waste detection is OCI-only and recommendation-only. It detects possible unused or wasteful OCI resources and writes findings into PostgreSQL. It does not delete, stop, resize, retag, or otherwise modify cloud resources.

AWS Cost Explorer and AWS CUR/S3 cost ingestion remain available, but AWS waste detection is planned for Phase 2 because it requires live inventory and utilization APIs such as EC2, EBS, ELB, RDS, CloudWatch, and multi-account/region discovery.

## Supported OCI Rules

- `OCI_DETACHED_BLOCK_VOLUME`: active block volumes with no active volume attachment.
- `OCI_DETACHED_BOOT_VOLUME`: active boot volumes with no active boot-volume attachment.
- `OCI_STOPPED_COMPUTE_WITH_PAID_STORAGE`: stopped compute instances with attached storage that may still generate cost.
- `OCI_UNUSED_RESERVED_PUBLIC_IP`: reserved public IPs with no assignment.

Old backup/snapshot detection is planned for a later phase because retention policies and compliance requirements need conservative handling.

## Configuration

```bash
TORVIX_WASTE_DETECTION_ENABLED=true
TORVIX_WASTE_PROVIDER=oci
TORVIX_WASTE_SCAN_INTERVAL_HOURS=24
TORVIX_WASTE_MIN_RESOURCE_AGE_DAYS=7
TORVIX_WASTE_STOPPED_INSTANCE_MIN_DAYS=3
TORVIX_WASTE_OLD_BACKUP_DAYS=30
TORVIX_WASTE_MIN_COST_THRESHOLD=0
TORVIX_WASTE_HIGH_MONTHLY_THRESHOLD=50
TORVIX_WASTE_CURRENCY=USD
TORVIX_WASTE_ENABLE_TAG_EXCLUSIONS=true
TORVIX_WASTE_EXCLUSION_TAG_KEYS=torvix:ignore,torvix:waste-ignore,finops:ignore,keep,retain,do-not-delete
```

Existing `CLOUDPULSE_WASTE_*` names are accepted only as lower-priority compatibility fallbacks where environment fallback support exists. New deployments should use `TORVIX_*`.

## Cost Correlation

Waste findings correlate OCI inventory with existing `cost_records` by `provider` and `resource_id`.

- `last_7d_cost` is preferred.
- `estimated_monthly_waste = last_7d_cost / 7 * 30`.
- If 7-day cost is missing but 30-day cost exists, Torvix uses the 30-day cost.
- If cost is unavailable, high-confidence inventory signals can still create low-severity findings with evidence that cost data was unavailable.

## Exclusion Tags

When tag exclusions are enabled, resources with any configured exclusion tag key and a value of `true`, `yes`, or `1` are skipped. Default keys:

- `torvix:ignore`
- `torvix:waste-ignore`
- `finops:ignore`
- `keep`
- `retain`
- `do-not-delete`

## API Endpoints

- `GET /api/v1/waste/summary`
- `GET /api/v1/waste/findings`
- `GET /api/v1/waste/findings/{id}`
- `GET /api/v1/waste/rules`
- `PATCH /api/v1/waste/findings/{id}/status`

Finding filters:

- `provider`
- `region`
- `scope_id`
- `scope_name`
- `service`
- `resource_type`
- `rule_id`
- `severity`
- `status`
- `min_confidence`
- `min_estimated_monthly_waste`
- `limit`
- `offset`

Valid statuses:

- `open`
- `accepted`
- `ignored`
- `false_positive`
- `fixed`
- `resolved`

Example:

```bash
curl "http://localhost:8080/api/v1/waste/findings?provider=oci&status=open&limit=20"
curl -X PATCH http://localhost:8080/api/v1/waste/findings/42/status \
  -H 'Content-Type: application/json' \
  -d '{"status":"ignored"}'
```

## Grafana Panels

The waste APIs are Grafana-compatible through the Torvix API datasource. Recommended panels:

- Open waste findings count from `/api/v1/waste/summary`
- Estimated monthly waste from `/api/v1/waste/summary`
- Waste by severity from `findings_by_severity`
- Waste by service from `findings_by_service`
- Waste by region from `findings_by_region`
- Waste by compartment/scope from `findings_by_scope`
- Top waste findings table from `/api/v1/waste/findings?status=open`

Recommended top findings columns:

- Severity
- Provider
- Region
- Scope
- Service
- Resource Type
- Resource Name
- Rule
- Estimated Monthly Waste
- Confidence
- Recommendation
- Status
- Last Seen

## OCI Permissions

Torvix needs read/list permissions for:

- Compartments and resource inventory
- Compute instances
- Boot volumes
- Block volumes
- Volume attachments
- Boot volume attachments
- Reserved public IPs and related networking resources
- Object Storage cost report bucket, as already required by cost ingestion

Monitoring metrics are not required for Phase 1 because no metrics-based idle compute rules are implemented.

## AWS Phase 2 TODO

AWS waste detection should add:

- EC2 stopped/idle instances
- Unattached EBS volumes
- Unused Elastic IPs
- Idle load balancers
- Idle RDS databases
- CloudWatch utilization metrics
- AWS tag-based exclusions
- Multi-account/region scanning
