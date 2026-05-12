package postgres

import (
	"context"
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
	mock.ExpectQuery("FROM cost_summary_daily").
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

func TestIsReportProcessed(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer mock.Close()

	mock.ExpectQuery("FROM processed_report_files").
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
