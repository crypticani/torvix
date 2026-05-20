package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	pgxmock "github.com/pashagolub/pgxmock/v4"

	"github.com/crypticani/cloudpulse/internal/domain"
)

func TestAggregateCostsUsesDailySummary(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"bucket", "cloud_provider", "account_id", "service", "total_cost"}).
		AddRow(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), "oci", "acct", "Compute", 12.5)
	mock.ExpectQuery("FROM cost_records").
		WithArgs(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)).
		WillReturnRows(rows)

	repo := NewWithDB(mock)
	out, err := repo.AggregateCosts(context.Background(), time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC), "daily")
	if err != nil {
		t.Fatalf("AggregateCosts() error = %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 row, got %d", len(out))
	}
	if out[0].Provider != "oci" || out[0].TotalCost != 12.5 {
		t.Fatalf("unexpected aggregate row: %+v", out[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestRefreshDashboardAnalyticsRecomputesAffectedWindowAndPrunesRetention(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer mock.Close()

	from := time.Date(2026, 5, 3, 15, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 4, 1, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM daily_cost_summaries").
		WithArgs(time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec("INSERT INTO daily_cost_summaries").
		WithArgs(
			time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 2))
	mock.ExpectExec("DELETE FROM weekly_cost_summaries").
		WithArgs(time.Date(2026, 4, 27, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec("INSERT INTO weekly_cost_summaries").
		WithArgs(
			time.Date(2026, 4, 27, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("DELETE FROM monthly_cost_summaries").
		WithArgs(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec("INSERT INTO monthly_cost_summaries").
		WithArgs(
			time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("DELETE FROM cost_anomalies").
		WithArgs(time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)).
		WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectExec("INSERT INTO cost_anomalies").
		WithArgs(time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 26, 0, 0, 0, 0, time.UTC)).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("DELETE FROM cost_forecasts WHERE forecast_date >=").
		WithArgs(pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectExec("INSERT INTO cost_forecasts").
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 7))
	mock.ExpectExec("DELETE FROM daily_cost_summaries WHERE period_start <").WithArgs(pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectExec("DELETE FROM weekly_cost_summaries WHERE period_start <").WithArgs(pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectExec("DELETE FROM monthly_cost_summaries WHERE period_start <").WithArgs(pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectExec("DELETE FROM cost_anomalies WHERE period_start <").WithArgs(pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectExec("DELETE FROM cost_forecasts WHERE forecast_date <").WithArgs(pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectCommit()

	repo := NewWithDB(mock)
	if err := repo.RefreshDashboardAnalytics(context.Background(), from, to, 90); err != nil {
		t.Fatalf("RefreshDashboardAnalytics() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDashboardAnalyticsSQLUsesExplicitTimestampBoundaries(t *testing.T) {
	b, err := os.ReadFile("repository_dashboard.go")
	if err != nil {
		t.Fatalf("read repository_dashboard.go: %v", err)
	}
	sql := string(b)
	for _, bad := range []string{
		">= $1 - INTERVAL",
		"< $2 - INTERVAL",
		">= NOW() - INTERVAL",
		"< NOW() - INTERVAL",
		">= NOW() - make_interval",
		"< NOW() - make_interval",
		">= INTERVAL",
		"< INTERVAL",
	} {
		if strings.Contains(sql, bad) {
			t.Fatalf("dashboard analytics SQL must compare timestamps to explicit timestamp parameters, found %q", bad)
		}
	}
}

func TestDashboardCostTimeseriesReadsPrecomputedDailySummaries(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer mock.Close()

	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)
	rows := pgxmock.NewRows([]string{"period_start", "period_end", "provider", "account_id", "service", "category", "region", "total_cost", "previous_period_cost", "absolute_change", "percentage_change", "updated_at"}).
		AddRow(from, from.AddDate(0, 0, 1), "oci", "acct", "Compute", "compute", "us-ashburn-1", 12.5, 10.0, 2.5, 25.0, time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC))
	mock.ExpectQuery("FROM daily_cost_summaries").
		WithArgs(from, to).
		WillReturnRows(rows)

	repo := NewWithDB(mock)
	out, err := repo.DashboardCostSummaries(context.Background(), "daily", from, to)
	if err != nil {
		t.Fatalf("DashboardCostSummaries() error = %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 row, got %d", len(out))
	}
	if out[0].Category != "compute" || out[0].Region != "us-ashburn-1" || out[0].PercentageChange != 25 {
		t.Fatalf("unexpected dashboard summary row: %+v", out[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestIsReportProcessed(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer mock.Close()

	mock.ExpectQuery("FROM processed_reports").
		WithArgs(domain.ProviderOCI, "bucket", "reports/001.csv.gz", "etag-1").
		WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))

	repo := NewWithDB(mock)
	ok, err := repo.IsReportProcessed(context.Background(), domain.ProviderOCI, "bucket", "reports/001.csv.gz", "etag-1")
	if err != nil {
		t.Fatalf("IsReportProcessed() error = %v", err)
	}
	if !ok {
		t.Fatalf("expected file to be marked processed")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDeleteCostRecordsForSourceDecompressesExistingSourceChunks(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer mock.Close()

	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT MIN\("timestamp"\), MAX\("timestamp"\)`).
		WithArgs(domain.ProviderOCI, "reports/001.csv.gz").
		WillReturnRows(pgxmock.NewRows([]string{"min", "max"}).AddRow(from, to))
	mock.ExpectQuery("decompress_chunk").
		WithArgs(from.Add(-time.Nanosecond), to.Add(time.Nanosecond)).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(1)))
	mock.ExpectExec("DELETE FROM cost_records").
		WithArgs(domain.ProviderOCI, "reports/001.csv.gz").
		WillReturnResult(pgxmock.NewResult("DELETE", 3))

	repo := NewWithDB(mock)
	if err := repo.DeleteCostRecordsForSource(context.Background(), domain.ProviderOCI, "reports/001.csv.gz"); err != nil {
		t.Fatalf("DeleteCostRecordsForSource() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDeleteCostRecordsForSourceSkipsDecompressionWhenSourceIsAbsent(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer mock.Close()

	mock.ExpectQuery(`SELECT MIN\("timestamp"\), MAX\("timestamp"\)`).
		WithArgs(domain.ProviderOCI, "reports/missing.csv.gz").
		WillReturnRows(pgxmock.NewRows([]string{"min", "max"}).AddRow(nil, nil))
	mock.ExpectExec("DELETE FROM cost_records").
		WithArgs(domain.ProviderOCI, "reports/missing.csv.gz").
		WillReturnResult(pgxmock.NewResult("DELETE", 0))

	repo := NewWithDB(mock)
	if err := repo.DeleteCostRecordsForSource(context.Background(), domain.ProviderOCI, "reports/missing.csv.gz"); err != nil {
		t.Fatalf("DeleteCostRecordsForSource() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
