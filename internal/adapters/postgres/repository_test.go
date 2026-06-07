package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	pgxmock "github.com/pashagolub/pgxmock/v4"

	"github.com/crypticani/torvix/internal/domain"
	"github.com/crypticani/torvix/internal/waste"
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

func TestAnomalySQLRequiresMeaningfulAbsoluteDeltaAndPositiveBaselineForPercentDeviation(t *testing.T) {
	b, err := os.ReadFile("repository.go")
	if err != nil {
		t.Fatalf("read repository.go: %v", err)
	}
	sql := string(b)
	if !strings.Contains(sql, "ABS(total_cost - baseline) >= 1") {
		t.Fatal("anomaly SQL must require at least INR 1 absolute delta before flagging a row")
	}
	if !strings.Contains(sql, "baseline > 0") {
		t.Fatal("anomaly SQL must require a positive baseline before using percentage deviation")
	}
	if strings.Contains(sql, "ABS(total_cost - baseline) >= baseline * 0.25") {
		t.Fatal("anomaly SQL must not compare absolute delta against raw baseline because zero or negative baselines create false anomalies")
	}
}

func TestAnomalySQLUsesPrecomputedSummariesWithCompartmentAndRegionDimensions(t *testing.T) {
	b, err := os.ReadFile("repository_dashboard.go")
	if err != nil {
		t.Fatalf("read repository_dashboard.go: %v", err)
	}
	sql := string(b)
	for _, want := range []string{
		"FROM daily_cost_summaries",
		"compartment_id",
		"compartment_name",
		"currency",
		"GROUP BY period_start, provider, account_id, compartment_id, compartment_name, service, region, currency",
		"prior.period_start >= current.period_start - INTERVAL '7 days'",
		"prior.compartment_id = current.compartment_id",
		"prior.compartment_name = current.compartment_name",
		"prior.currency = current.currency",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("anomaly SQL must include locality dimension %q", want)
		}
	}
	for _, forbidden := range []string{
		"PARTITION BY provider, account_id, service, category, region",
		"ROWS BETWEEN 7 PRECEDING AND 1 PRECEDING",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("anomaly SQL must not mix dimensions using %q", forbidden)
		}
	}
}

func TestAWSCostRecordsUseIdempotentUpsertKey(t *testing.T) {
	b, err := os.ReadFile("repository.go")
	if err != nil {
		t.Fatalf("read repository.go: %v", err)
	}
	sql := string(b)
	for _, want := range []string{
		`ON CONFLICT ("timestamp", cloud_provider, region, billing_scope_type, billing_scope_id, service, record_type)`,
		`record_type <> 'cur_line_item'`,
		`ON CONFLICT ("timestamp", cloud_provider, record_type, source_file_key, source_record_hash)`,
		`record_type = 'cur_line_item'`,
		`source_file_key <> ''`,
		`WHERE cloud_provider = 'aws'`,
		`raw_metadata = EXCLUDED.raw_metadata`,
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("AWS cost record upsert SQL must contain %q", want)
		}
	}
}

func TestListCloudResourcesOnlyReturnsActiveResources(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer mock.Close()

	rows := pgxmock.NewRows([]string{
		"id", "provider", "resource_id", "resource_name", "resource_type", "region", "scope_id", "scope_name",
		"lifecycle_state", "availability_domain", "time_created", "tags", "raw", "first_seen_at", "last_seen_at",
	})
	mock.ExpectQuery("WHERE provider = \\$1\\s+AND active = TRUE").
		WithArgs(domain.ProviderOCI).
		WillReturnRows(rows)

	repo := NewWithDB(mock)
	got, err := repo.ListCloudResources(context.Background(), domain.ProviderOCI)
	if err != nil {
		t.Fatalf("ListCloudResources() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no active resources, got %d", len(got))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestMarkMissingCloudResourcesInactiveOnlyAfterSuccessfulRun(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer mock.Close()

	mock.ExpectExec("UPDATE cloud_resources").
		WithArgs(domain.ProviderOCI, "ap-mumbai-1", "run-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 2))

	repo := NewWithDB(mock)
	count, err := repo.MarkMissingCloudResourcesInactive(context.Background(), domain.ProviderOCI, "ap-mumbai-1", "run-1")
	if err != nil {
		t.Fatalf("MarkMissingCloudResourcesInactive() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 inactive resources, got %d", count)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestReplaceCloudRelationshipsScopedOnlyDeletesCompletedScopeType(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer mock.Close()

	scope := waste.RelationshipScope{
		Provider:         domain.ProviderOCI,
		Region:           "ap-mumbai-1",
		ScopeID:          "compartment-1",
		RelationshipType: waste.RelationshipBlockVolumeAttachedToInstance,
	}
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM cloud_resource_relationships").
		WithArgs(scope.Provider, scope.RelationshipType, scope.Region, scope.ScopeID).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec("INSERT INTO cloud_resource_relationships").
		WithArgs(domain.ProviderOCI, "volume-1", "instance-1", waste.RelationshipBlockVolumeAttachedToInstance, "ap-mumbai-1", "compartment-1", pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	repo := NewWithDB(mock)
	err = repo.ReplaceCloudRelationshipsScoped(context.Background(), scope, []waste.Relationship{{
		Provider:         domain.ProviderOCI,
		SourceResourceID: "volume-1",
		TargetResourceID: "instance-1",
		RelationshipType: waste.RelationshipBlockVolumeAttachedToInstance,
		Region:           "ap-mumbai-1",
		ScopeID:          "compartment-1",
		DetectedAt:       time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC),
		Raw:              map[string]any{"attachment_id": "attachment-1"},
	}})
	if err != nil {
		t.Fatalf("ReplaceCloudRelationshipsScoped() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestReplaceCloudRelationshipsScopedRollsBackOnInsertFailure(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer mock.Close()

	scope := waste.RelationshipScope{
		Provider:         domain.ProviderOCI,
		Region:           "ap-mumbai-1",
		ScopeID:          "compartment-1",
		RelationshipType: waste.RelationshipBlockVolumeAttachedToInstance,
	}
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM cloud_resource_relationships").
		WithArgs(scope.Provider, scope.RelationshipType, scope.Region, scope.ScopeID).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec("INSERT INTO cloud_resource_relationships").
		WithArgs(domain.ProviderOCI, "volume-1", "instance-1", waste.RelationshipBlockVolumeAttachedToInstance, "ap-mumbai-1", "compartment-1", pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(os.ErrInvalid)
	mock.ExpectRollback()

	repo := NewWithDB(mock)
	err = repo.ReplaceCloudRelationshipsScoped(context.Background(), scope, []waste.Relationship{{
		Provider:         domain.ProviderOCI,
		SourceResourceID: "volume-1",
		TargetResourceID: "instance-1",
		RelationshipType: waste.RelationshipBlockVolumeAttachedToInstance,
		Region:           "ap-mumbai-1",
		ScopeID:          "compartment-1",
		DetectedAt:       time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC),
	}})
	if err == nil {
		t.Fatal("expected insert failure")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAWSAnalyticsQueriesPreferCURWhenPresent(t *testing.T) {
	for _, path := range []string{"repository.go", "repository_dashboard.go"} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		sql := string(b)
		for _, want := range []string{
			`record_type = 'cur_line_item'`,
			`record_type = 'linked_account_service'`,
			`NOT EXISTS`,
		} {
			if !strings.Contains(sql, want) {
				t.Fatalf("%s must contain canonical AWS source filter %q", path, want)
			}
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
	rows := pgxmock.NewRows([]string{"period_start", "period_end", "provider", "account_id", "compartment_id", "compartment_name", "billing_scope_type", "billing_scope_id", "billing_scope_name", "record_type", "service", "category", "region", "total_cost", "previous_period_cost", "absolute_change", "percentage_change", "updated_at"}).
		AddRow(from, from.AddDate(0, 0, 1), "oci", "acct", "ocid1.compartment.oc1..app", "app-prod", "compartment", "ocid1.compartment.oc1..app", "app-prod", "", "Compute", "compute", "us-ashburn-1", 12.5, 10.0, 2.5, 25.0, time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC))
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
	if out[0].Category != "compute" || out[0].CompartmentName != "app-prod" || out[0].Region != "us-ashburn-1" || out[0].PercentageChange != 25 {
		t.Fatalf("unexpected dashboard summary row: %+v", out[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDashboardOverviewUsesLatestProcessedReportWhenCheckpointIsUnchanged(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer mock.Close()

	currentFrom := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	currentTo := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	previousFrom := time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC)
	previousTo := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	latestProcessed := time.Date(2026, 5, 20, 3, 27, 9, 0, time.UTC)
	rows := pgxmock.NewRows([]string{"current", "previous", "change", "anomalies", "latest_ingestion"}).
		AddRow(100.0, 50.0, 100.0, int64(2), latestProcessed)
	mock.ExpectQuery("FROM processed_reports").
		WithArgs(currentFrom, currentTo, previousFrom, previousTo).
		WillReturnRows(rows)

	repo := NewWithDB(mock)
	out, err := repo.DashboardOverview(context.Background(), currentFrom, currentTo, previousFrom, previousTo)
	if err != nil {
		t.Fatalf("DashboardOverview() error = %v", err)
	}
	if !out.LatestIngestionAt.Equal(latestProcessed) {
		t.Fatalf("expected latest processed report time %s, got %s", latestProcessed, out.LatestIngestionAt)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestLatestIngestionStatusUsesLatestProcessedReportForSummaryTimestamp(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer mock.Close()

	checkpoint := time.Unix(0, 0).UTC()
	updatedAt := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	latestProcessed := time.Date(2026, 5, 20, 3, 27, 9, 0, time.UTC)
	rows := pgxmock.NewRows([]string{"provider", "last_successful_ingestion_at", "updated_at", "latest_report_processed_at", "files_processed", "records_processed", "status", "error_message"}).
		AddRow(domain.ProviderOCI, checkpoint, updatedAt, latestProcessed, int64(25), int64(564983), "processed", "")
	mock.ExpectQuery("WITH providers AS").
		WillReturnRows(rows)

	repo := NewWithDB(mock)
	out, err := repo.LatestIngestionStatus(context.Background())
	if err != nil {
		t.Fatalf("LatestIngestionStatus() error = %v", err)
	}
	if !out.LatestIngestionAt.Equal(latestProcessed) {
		t.Fatalf("expected summary latest ingestion %s, got %s", latestProcessed, out.LatestIngestionAt)
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

func TestReportDeliveryLedger(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer mock.Close()

	from := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	key := domain.ReportDeliveryKey{Provider: "all", ReportType: "weekly", PeriodStart: from, PeriodEnd: to, Destination: "slack"}
	mock.ExpectQuery("FROM report_deliveries").
		WithArgs("all", "weekly", from, to, "slack").
		WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectExec("INSERT INTO report_deliveries").
		WithArgs("all", "weekly", from, to, "slack").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	repo := NewWithDB(mock)
	ok, err := repo.IsReportDelivered(context.Background(), key)
	if err != nil {
		t.Fatalf("IsReportDelivered() error = %v", err)
	}
	if !ok {
		t.Fatalf("expected weekly report delivery to exist")
	}
	if err := repo.RecordReportDelivery(context.Background(), key); err != nil {
		t.Fatalf("RecordReportDelivery() error = %v", err)
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
