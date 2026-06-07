package postgres

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/crypticani/torvix/internal/domain"
)

func TestMigration011UpgradesCompressedLegacyHypertable(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TORVIX_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set TORVIX_TEST_DATABASE_URL to run TimescaleDB migration integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect TimescaleDB: %v", err)
	}
	defer admin.Close()

	if _, err := admin.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS timescaledb`); err != nil {
		t.Fatalf("ensure TimescaleDB extension: %v", err)
	}

	schema := fmt.Sprintf("torvix_migration_%d", time.Now().UnixNano())
	schemaSQL := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schemaSQL); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, _ = admin.Exec(cleanupCtx, "DROP SCHEMA IF EXISTS "+schemaSQL+" CASCADE")
	})

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse TimescaleDB DSN: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect isolated test schema: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, legacyMigration011FixtureSQL); err != nil {
		t.Fatalf("create legacy migration fixture: %v", err)
	}

	migrationDir := t.TempDir()
	migrationNames := []string{
		"011_provider_agnostic_cost_dimensions.sql",
		"016_aws_cur_idempotency.sql",
	}
	for _, migrationName := range migrationNames {
		migration, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", migrationName))
		if err != nil {
			t.Fatalf("read %s: %v", migrationName, err)
		}
		if err := os.WriteFile(filepath.Join(migrationDir, migrationName), migration, 0o600); err != nil {
			t.Fatalf("write isolated %s: %v", migrationName, err)
		}
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := NewMigratorWithLogger(pool, migrationDir, logger).Run(ctx); err != nil {
		t.Fatalf("run migration 011 against compressed legacy hypertable: %v", err)
	}

	for _, migrationName := range migrationNames {
		var applied bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM schema_migrations WHERE version = $1
		)`, migrationName).Scan(&applied); err != nil {
			t.Fatalf("check %s record: %v", migrationName, err)
		}
		if !applied {
			t.Fatalf("%s was not recorded as applied", migrationName)
		}
	}

	var scopeType, scopeID, scopeName string
	if err := pool.QueryRow(ctx, `
		SELECT billing_scope_type, billing_scope_id, billing_scope_name
		FROM cost_records
		WHERE cloud_provider = 'oci'
	`).Scan(&scopeType, &scopeID, &scopeName); err != nil {
		t.Fatalf("read OCI backfill: %v", err)
	}
	if scopeType != "compartment" || scopeID != "ocid1.compartment.oc1..example" || scopeName != "Production" {
		t.Fatalf("unexpected OCI billing scope backfill: type=%q id=%q name=%q", scopeType, scopeID, scopeName)
	}

	expectedDefaults := map[string]string{
		"billing_scope_type": "''::text",
		"raw_metadata":       "'{}'::jsonb",
		"source_line_number": "0",
	}
	for column, expectedDefault := range expectedDefaults {
		var nullable, defaultValue string
		if err := pool.QueryRow(ctx, `
			SELECT is_nullable, column_default
			FROM information_schema.columns
			WHERE table_schema = $1
			  AND table_name = 'cost_records'
			  AND column_name = $2
		`, schema, column).Scan(&nullable, &defaultValue); err != nil {
			t.Fatalf("read %s column metadata: %v", column, err)
		}
		if nullable != "YES" {
			t.Fatalf("%s is_nullable = %q, want YES", column, nullable)
		}
		if defaultValue != expectedDefault {
			t.Fatalf("%s default = %q, want %q", column, defaultValue, expectedDefault)
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin AWS idempotency verification: %v", err)
	}
	records := []domain.CanonicalCostRecord{
		{
			Timestamp:        time.Now().UTC(),
			Provider:         domain.ProviderAWS,
			AccountID:        "123456789012",
			Service:          "AmazonEC2",
			Category:         "Compute",
			BillingScopeType: "linked_account",
			BillingScopeID:   "123456789012",
			Region:           "us-east-1",
			Cost:             10,
			Currency:         "USD",
			RecordType:       "cost_explorer_daily",
		},
	}
	records = append(records, records[0])
	curTimestamp := time.Now().UTC().Add(time.Second)
	records = append(records,
		domain.CanonicalCostRecord{
			Timestamp:        curTimestamp,
			Provider:         domain.ProviderAWS,
			AccountID:        "123456789012",
			Service:          "AmazonS3",
			Category:         "Storage",
			BillingScopeType: "linked_account",
			BillingScopeID:   "123456789012",
			Region:           "us-east-1",
			Cost:             1,
			Currency:         "USD",
			RecordType:       "cur_line_item",
			SourceFileKey:    "exports/part-1.csv.gz",
			SourceRecordHash: "same-logical-line",
		},
		domain.CanonicalCostRecord{
			Timestamp:        curTimestamp,
			Provider:         domain.ProviderAWS,
			AccountID:        "123456789012",
			Service:          "AmazonS3",
			Category:         "Storage",
			BillingScopeType: "linked_account",
			BillingScopeID:   "123456789012",
			Region:           "us-east-1",
			Cost:             2,
			Currency:         "USD",
			RecordType:       "cur_line_item",
			SourceFileKey:    "exports/part-2.csv.gz",
			SourceRecordHash: "same-logical-line",
		},
	)
	if err := storeCostRecordsTx(ctx, tx, records); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("store AWS records after idempotency migration: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit AWS idempotency verification: %v", err)
	}

	var awsRows int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM cost_records WHERE cloud_provider = 'aws'`).Scan(&awsRows); err != nil {
		t.Fatalf("count AWS idempotency rows: %v", err)
	}
	if awsRows != 3 {
		t.Fatalf("AWS rows = %d, want 3 (one generic aggregate and two source-scoped CUR rows)", awsRows)
	}
}

const legacyMigration011FixtureSQL = `
CREATE TABLE schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO schema_migrations(version) VALUES
    ('001_init.sql'),
    ('002_processed_report_files.sql'),
    ('003_processed_reports.sql'),
    ('004_ingestion_checkpoints_lifecycle.sql'),
    ('005_cost_records_source_object_index.sql'),
    ('006_dashboard_analytics.sql'),
    ('007_dashboard_compartment_summaries.sql'),
    ('008_rebuild_dashboard_compartment_summaries.sql'),
    ('009_report_deliveries.sql'),
    ('010_report_delivery_dimensions.sql');

CREATE TABLE cost_records
(
    id BIGSERIAL,
    "timestamp" TIMESTAMPTZ NOT NULL,
    cloud_provider TEXT NOT NULL,
    account_id TEXT,
    service TEXT NOT NULL,
    category TEXT NOT NULL,
    resource_id TEXT,
    region TEXT,
    usage_quantity DOUBLE PRECISION,
    usage_unit TEXT,
    cost NUMERIC(20,8),
    currency TEXT,
    tags JSONB NOT NULL DEFAULT '{}'::jsonb,
    raw_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    source_object TEXT NOT NULL DEFAULT '',
    meter TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

SELECT create_hypertable(
    'cost_records',
    'timestamp',
    chunk_time_interval => INTERVAL '1 day',
    create_default_indexes => FALSE
);

ALTER TABLE cost_records SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'cloud_provider,account_id,service,category',
    timescaledb.compress_orderby = '"timestamp" DESC'
);

INSERT INTO cost_records (
    "timestamp",
    cloud_provider,
    account_id,
    service,
    category,
    region,
    cost,
    currency,
    tags
) VALUES (
    now() - INTERVAL '30 days',
    'oci',
    'ocid1.tenancy.oc1..example',
    'COMPUTE',
    'Compute',
    'ap-mumbai-1',
    12.34,
    'USD',
    '{"oci_compartment_id":"ocid1.compartment.oc1..example","oci_compartment_name":"Production"}'::jsonb
);

SELECT compress_chunk(chunk, if_not_compressed => TRUE)
FROM show_chunks('cost_records') AS chunk;

CREATE TABLE daily_cost_summaries (
    period_start TIMESTAMPTZ NOT NULL,
    provider TEXT NOT NULL,
    account_id TEXT NOT NULL DEFAULT '',
    compartment_id TEXT NOT NULL DEFAULT '',
    compartment_name TEXT NOT NULL DEFAULT '',
    service TEXT NOT NULL DEFAULT 'unknown',
    category TEXT NOT NULL DEFAULT 'uncategorized',
    region TEXT NOT NULL DEFAULT '',
    currency TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (period_start, provider, account_id, compartment_id, compartment_name, service, category, region, currency)
);

CREATE TABLE weekly_cost_summaries (LIKE daily_cost_summaries INCLUDING ALL);
CREATE TABLE monthly_cost_summaries (LIKE daily_cost_summaries INCLUDING ALL);
`
