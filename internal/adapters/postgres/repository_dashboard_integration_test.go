package postgres

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRefreshCostAnomaliesExecutesAgainstPostgres(t *testing.T) {
	dsn := testDatabaseURL()
	if dsn == "" {
		t.Skip("set TORVIX_TEST_DATABASE_URL or CLOUDPULSE_TEST_DATABASE_URL to run PostgreSQL anomaly SQL regression test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		CREATE TEMP TABLE daily_cost_summaries (
			period_start timestamptz NOT NULL,
			period_end timestamptz NOT NULL,
			provider text NOT NULL,
			account_id text NOT NULL DEFAULT '',
			compartment_id text NOT NULL DEFAULT '',
			compartment_name text NOT NULL DEFAULT '',
			service text NOT NULL DEFAULT 'unknown',
			category text NOT NULL DEFAULT 'uncategorized',
			region text NOT NULL DEFAULT '',
			currency text NOT NULL DEFAULT '',
			total_cost numeric(20,8) NOT NULL DEFAULT 0,
			previous_period_cost numeric(20,8) NOT NULL DEFAULT 0,
			absolute_change numeric(20,8) NOT NULL DEFAULT 0,
			percentage_change double precision NOT NULL DEFAULT 0,
			updated_at timestamptz NOT NULL DEFAULT now()
		) ON COMMIT DROP;

		CREATE TEMP TABLE cost_anomalies (
			id bigserial PRIMARY KEY,
			detected_at timestamptz NOT NULL DEFAULT now(),
			period_start timestamptz NOT NULL,
			provider text NOT NULL,
			account_id text NOT NULL DEFAULT '',
			category text NOT NULL DEFAULT 'uncategorized',
			service text NOT NULL DEFAULT 'unknown',
			region text NOT NULL DEFAULT '',
			observed_cost numeric(20,8) NOT NULL,
			expected_cost numeric(20,8) NOT NULL,
			absolute_delta numeric(20,8) NOT NULL,
			percentage_delta double precision NOT NULL,
			severity text NOT NULL,
			detection_method text NOT NULL,
			explanation text NOT NULL,
			created_at timestamptz NOT NULL DEFAULT now()
		) ON COMMIT DROP;
	`); err != nil {
		t.Fatalf("create temp analytics tables: %v", err)
	}

	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 8; i++ {
		cost := 10.0
		if i == 7 {
			cost = 25.0
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO daily_cost_summaries
			(period_start, period_end, provider, account_id, service, category, region, currency, total_cost)
			VALUES ($1, $2, 'oci', 'acct', 'Object Storage', 'storage', 'us-ashburn-1', 'USD', $3)
		`, base.AddDate(0, 0, i), base.AddDate(0, 0, i+1), cost)
		if err != nil {
			t.Fatalf("insert daily summary fixture: %v", err)
		}
	}

	err = refreshCostAnomalies(ctx, tx, slog.New(slog.NewTextHandler(io.Discard, nil)), base.AddDate(0, 0, 7), base.AddDate(0, 0, 8), base)
	if err != nil && strings.Contains(err.Error(), "timestamp with time zone >= interval") {
		t.Fatalf("regression: anomaly SQL compared timestamp with interval: %v", err)
	}
	if err != nil {
		t.Fatalf("refreshCostAnomalies() error = %v", err)
	}

	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM cost_anomalies`).Scan(&count); err != nil {
		t.Fatalf("count anomalies: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 anomaly, got %d", count)
	}
}

func TestPostgresReportsTimestampComparedToIntervalRegressionMessage(t *testing.T) {
	dsn := testDatabaseURL()
	if dsn == "" {
		t.Skip("set TORVIX_TEST_DATABASE_URL or CLOUDPULSE_TEST_DATABASE_URL to run PostgreSQL timestamp-vs-interval error regression test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	defer pool.Close()

	_, err = pool.Exec(ctx, `SELECT NOW() >= INTERVAL '7 days'`)
	if err == nil {
		t.Fatal("expected PostgreSQL to reject timestamp with time zone >= interval")
	}
	if !strings.Contains(err.Error(), "timestamp with time zone >= interval") {
		t.Fatalf("expected exact timestamp-vs-interval error, got %v", err)
	}
}

func testDatabaseURL() string {
	if dsn := strings.TrimSpace(os.Getenv("TORVIX_TEST_DATABASE_URL")); dsn != "" {
		return dsn
	}
	return strings.TrimSpace(os.Getenv("CLOUDPULSE_TEST_DATABASE_URL"))
}
