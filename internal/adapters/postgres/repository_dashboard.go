package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/crypticani/torvix/internal/domain"
)

func (r *Repository) RefreshDashboardAnalytics(ctx context.Context, from, to time.Time, retentionDays int) error {
	started := time.Now()
	now := time.Now().UTC()
	if retentionDays <= 0 {
		retentionDays = 90
	}
	if to.IsZero() {
		to = now
	}
	if from.IsZero() || from.After(to) {
		from = to.AddDate(0, 0, -30)
	}

	dailyFrom, dailyTo := alignDailyRefreshWindow(from.UTC(), to.UTC())
	weeklyFrom, weeklyTo := alignWeeklyRefreshWindow(from.UTC(), to.UTC())
	monthlyFrom, monthlyTo := alignMonthlyRefreshWindow(from.UTC(), to.UTC())
	dailyPreviousFrom, dailyPreviousTo := previousSummaryWindow("1 day", dailyFrom, dailyTo)
	weeklyPreviousFrom, weeklyPreviousTo := previousSummaryWindow("1 week", weeklyFrom, weeklyTo)
	monthlyPreviousFrom, monthlyPreviousTo := previousSummaryWindow("1 month", monthlyFrom, monthlyTo)
	anomalyBaselineFrom := dailyFrom.AddDate(0, 0, -7)
	forecastTo := dayStart(now)
	forecastFrom := forecastTo.AddDate(0, 0, -14)
	pruneCutoff := now.AddDate(0, 0, -retentionDays)

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	if err = refreshCostSummary(ctx, tx, r.logger, "daily_cost_summaries", "1 day", dailyFrom, dailyTo, dailyPreviousFrom, dailyPreviousTo); err != nil {
		r.logger.Error("dashboard analytics refresh phase failed", "phase", "daily_summary", "from", dailyFrom, "to", dailyTo, "error", err)
		return err
	}
	if err = refreshCostSummary(ctx, tx, r.logger, "weekly_cost_summaries", "1 week", weeklyFrom, weeklyTo, weeklyPreviousFrom, weeklyPreviousTo); err != nil {
		r.logger.Error("dashboard analytics refresh phase failed", "phase", "weekly_summary", "from", weeklyFrom, "to", weeklyTo, "error", err)
		return err
	}
	if err = refreshCostSummary(ctx, tx, r.logger, "monthly_cost_summaries", "1 month", monthlyFrom, monthlyTo, monthlyPreviousFrom, monthlyPreviousTo); err != nil {
		r.logger.Error("dashboard analytics refresh phase failed", "phase", "monthly_summary", "from", monthlyFrom, "to", monthlyTo, "error", err)
		return err
	}
	if err = refreshCostAnomalies(ctx, tx, r.logger, dailyFrom, dailyTo, anomalyBaselineFrom); err != nil {
		r.logger.Error("dashboard analytics refresh phase failed", "phase", "anomalies", "from", dailyFrom, "to", dailyTo, "baseline_from", anomalyBaselineFrom, "error", err)
		return err
	}
	if err = refreshCostForecasts(ctx, tx, r.logger, forecastFrom, forecastTo); err != nil {
		r.logger.Error("dashboard analytics refresh phase failed", "phase", "forecasts", "from", forecastFrom, "to", forecastTo, "error", err)
		return err
	}
	if err = pruneDashboardAnalytics(ctx, tx, pruneCutoff); err != nil {
		r.logger.Error("dashboard analytics refresh phase failed", "phase", "prune", "cutoff", pruneCutoff, "error", err)
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}

	r.logger.Info("dashboard analytics refresh completed", "from", dailyFrom, "to", dailyTo, "retention_days", retentionDays, "duration", time.Since(started).String())
	return nil
}

func refreshCostSummary(ctx context.Context, tx pgx.Tx, logger *slog.Logger, table, interval string, from, to, previousFrom, previousTo time.Time) error {
	if !to.After(from) {
		return nil
	}
	deleteSQL := fmt.Sprintf(`DELETE FROM %s WHERE period_start >= $1 AND period_start < $2`, table)
	if _, err := tx.Exec(ctx, deleteSQL, from, to); err != nil {
		return fmt.Errorf("delete %s window: %w", table, err)
	}

	insertSQL := fmt.Sprintf(`
		WITH current_period AS (
			SELECT
				time_bucket(INTERVAL '%s', "timestamp") AS period_start,
				cloud_provider AS provider,
				COALESCE(account_id, '') AS account_id,
				COALESCE(NULLIF(billing_scope_type, ''), CASE WHEN cloud_provider = 'oci' THEN 'compartment' ELSE '' END) AS billing_scope_type,
				COALESCE(NULLIF(billing_scope_id, ''), tags->>'oci_compartment_id', '') AS billing_scope_id,
				COALESCE(NULLIF(billing_scope_name, ''), NULLIF(tags->>'oci_compartment_name', ''), tags->>'oci_compartment_id', '') AS billing_scope_name,
				COALESCE(NULLIF(billing_scope_id, ''), tags->>'oci_compartment_id', '') AS compartment_id,
				COALESCE(NULLIF(billing_scope_name, ''), NULLIF(tags->>'oci_compartment_name', ''), tags->>'oci_compartment_id', '') AS compartment_name,
				COALESCE(record_type, '') AS record_type,
				COALESCE(service, 'unknown') AS service,
				COALESCE(category, 'uncategorized') AS category,
				COALESCE(region, '') AS region,
				COALESCE(currency, '') AS currency,
				COALESCE(SUM(cost), 0)::double precision AS total_cost
			FROM cost_records
			WHERE "timestamp" >= $1 AND "timestamp" < $2
			  AND (cloud_provider <> 'aws' OR record_type IN ('linked_account_service', 'region_service', 'cur_line_item'))
			GROUP BY 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13
		),
		previous_period AS (
			SELECT
				time_bucket(INTERVAL '%s', "timestamp") + INTERVAL '%s' AS period_start,
				cloud_provider AS provider,
				COALESCE(account_id, '') AS account_id,
				COALESCE(NULLIF(billing_scope_type, ''), CASE WHEN cloud_provider = 'oci' THEN 'compartment' ELSE '' END) AS billing_scope_type,
				COALESCE(NULLIF(billing_scope_id, ''), tags->>'oci_compartment_id', '') AS billing_scope_id,
				COALESCE(NULLIF(billing_scope_name, ''), NULLIF(tags->>'oci_compartment_name', ''), tags->>'oci_compartment_id', '') AS billing_scope_name,
				COALESCE(NULLIF(billing_scope_id, ''), tags->>'oci_compartment_id', '') AS compartment_id,
				COALESCE(NULLIF(billing_scope_name, ''), NULLIF(tags->>'oci_compartment_name', ''), tags->>'oci_compartment_id', '') AS compartment_name,
				COALESCE(record_type, '') AS record_type,
				COALESCE(service, 'unknown') AS service,
				COALESCE(category, 'uncategorized') AS category,
				COALESCE(region, '') AS region,
				COALESCE(currency, '') AS currency,
				COALESCE(SUM(cost), 0)::double precision AS total_cost
			FROM cost_records
			WHERE "timestamp" >= $3 AND "timestamp" < $4
			  AND (cloud_provider <> 'aws' OR record_type IN ('linked_account_service', 'region_service', 'cur_line_item'))
			GROUP BY 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13
		)
		INSERT INTO %s
		(period_start, period_end, provider, account_id, compartment_id, compartment_name, billing_scope_type, billing_scope_id, billing_scope_name, record_type, service, category, region, currency, total_cost, previous_period_cost, absolute_change, percentage_change, updated_at)
		SELECT
			c.period_start,
			c.period_start + INTERVAL '%s',
			c.provider,
			c.account_id,
			c.compartment_id,
			c.compartment_name,
			c.billing_scope_type,
			c.billing_scope_id,
			c.billing_scope_name,
			c.record_type,
			c.service,
			c.category,
			c.region,
			c.currency,
			c.total_cost,
			COALESCE(p.total_cost, 0)::double precision,
			(c.total_cost - COALESCE(p.total_cost, 0))::double precision,
			CASE
				WHEN COALESCE(p.total_cost, 0) > 0 THEN ((c.total_cost - p.total_cost) / p.total_cost) * 100
				WHEN c.total_cost > 0 THEN 100
				ELSE 0
			END::double precision,
			NOW()
		FROM current_period c
		LEFT JOIN previous_period p
		  ON p.period_start = c.period_start
		 AND p.provider = c.provider
		 AND p.account_id = c.account_id
		 AND p.compartment_id = c.compartment_id
		 AND p.compartment_name = c.compartment_name
		 AND p.billing_scope_type = c.billing_scope_type
		 AND p.billing_scope_id = c.billing_scope_id
		 AND p.billing_scope_name = c.billing_scope_name
		 AND p.record_type = c.record_type
		 AND p.service = c.service
		 AND p.category = c.category
		 AND p.region = c.region
		 AND p.currency = c.currency
	`, interval, interval, interval, table, interval)
	if _, err := tx.Exec(ctx, insertSQL, from, to, previousFrom, previousTo); err != nil {
		return fmt.Errorf("insert %s window: %w", table, err)
	}
	logger.Info("dashboard summary recomputed", "table", table, "from", from, "to", to)
	return nil
}

func refreshCostAnomalies(ctx context.Context, tx pgx.Tx, logger *slog.Logger, from, to, baselineFrom time.Time) error {
	if !to.After(from) {
		return nil
	}
	if _, err := tx.Exec(ctx, `DELETE FROM cost_anomalies WHERE period_start >= $1 AND period_start < $2`, from, to); err != nil {
		return fmt.Errorf("delete cost anomalies window: %w", err)
	}
	_, err := tx.Exec(ctx, `
		WITH series AS (
			SELECT
				period_start,
				provider,
				account_id,
				service,
				category,
				region,
				total_cost,
				AVG(total_cost) OVER w AS expected_cost,
				STDDEV_POP(total_cost) OVER w AS stddev
			FROM daily_cost_summaries d
			WHERE d.period_start >= $3 AND d.period_start < $2
			  AND (
				d.provider <> 'aws'
				OR d.record_type = 'cur_line_item'
				OR (
					d.record_type = 'linked_account_service'
					AND NOT EXISTS (
						SELECT 1
						FROM daily_cost_summaries aws_cur
						WHERE aws_cur.provider = 'aws'
						  AND aws_cur.record_type = 'cur_line_item'
						  AND aws_cur.period_start = d.period_start
					)
				)
			  )
			WINDOW w AS (
				PARTITION BY provider, account_id, service, category, region
				ORDER BY period_start
				ROWS BETWEEN 7 PRECEDING AND 1 PRECEDING
			)
		),
		scored AS (
			SELECT
				period_start,
				provider,
				account_id,
				service,
				category,
				region,
				total_cost AS observed_cost,
				expected_cost,
				(total_cost - expected_cost) AS absolute_delta,
				CASE WHEN expected_cost > 0 THEN ((total_cost - expected_cost) / expected_cost) * 100 ELSE 0 END AS percentage_delta,
				CASE WHEN COALESCE(stddev, 0) > 0 THEN (total_cost - expected_cost) / stddev ELSE 0 END AS z_score
			FROM series
			WHERE period_start >= $1
			  AND period_start < $2
			  AND expected_cost IS NOT NULL
		)
		INSERT INTO cost_anomalies
		(detected_at, period_start, provider, account_id, category, service, region, observed_cost, expected_cost, absolute_delta, percentage_delta, severity, detection_method, explanation, created_at)
		SELECT
			NOW(),
			period_start,
			provider,
			account_id,
			category,
			service,
			region,
			observed_cost,
			expected_cost,
			absolute_delta,
			percentage_delta,
			CASE
				WHEN ABS(percentage_delta) >= 50 OR ABS(z_score) >= 3 THEN 'high'
				WHEN ABS(percentage_delta) >= 30 OR ABS(z_score) >= 2 THEN 'medium'
				ELSE 'low'
			END,
			'trailing_7_day_baseline',
			concat(upper(provider), ' ', service, ' daily spend was ', round(percentage_delta::numeric, 1), '% ',
				CASE WHEN percentage_delta >= 0 THEN 'above' ELSE 'below' END,
				' its trailing baseline: observed ', round(observed_cost::numeric, 2), ', expected ', round(expected_cost::numeric, 2), '.'),
			NOW()
		FROM scored
		WHERE ABS(absolute_delta) >= 1
		  AND (ABS(percentage_delta) >= 30 OR ABS(z_score) >= 2)
	`, from, to, baselineFrom)
	if err != nil {
		return fmt.Errorf("insert cost anomalies window: %w", err)
	}
	logger.Info("dashboard anomalies recomputed", "from", from, "to", to)
	return nil
}

func refreshCostForecasts(ctx context.Context, tx pgx.Tx, logger *slog.Logger, from, to time.Time) error {
	if !to.After(from) {
		return nil
	}
	if _, err := tx.Exec(ctx, `DELETE FROM cost_forecasts WHERE forecast_date >= $1::date`, to); err != nil {
		return fmt.Errorf("delete cost forecasts: %w", err)
	}
	_, err := tx.Exec(ctx, `
		WITH ranked AS (
			SELECT
				period_start::date AS period_date,
				provider,
				account_id,
				category,
				service,
				region,
				total_cost,
				ROW_NUMBER() OVER (
					PARTITION BY provider, account_id, category, service, region
					ORDER BY period_start DESC
				) AS rn
			FROM daily_cost_summaries d
			WHERE d.period_start >= $1
			  AND d.period_start < $2
			  AND (
				d.provider <> 'aws'
				OR d.record_type = 'cur_line_item'
				OR (
					d.record_type = 'linked_account_service'
					AND NOT EXISTS (
						SELECT 1
						FROM daily_cost_summaries aws_cur
						WHERE aws_cur.provider = 'aws'
						  AND aws_cur.record_type = 'cur_line_item'
						  AND aws_cur.period_start = d.period_start
					)
				)
			  )
		),
		baseline AS (
			SELECT
				provider,
				account_id,
				category,
				service,
				region,
				AVG(total_cost)::double precision AS forecast_cost,
				COALESCE(STDDEV_POP(total_cost), 0)::double precision AS sigma
			FROM ranked
			WHERE rn <= 7
			GROUP BY 1, 2, 3, 4, 5
		)
		INSERT INTO cost_forecasts
		(forecast_date, generated_at, provider, account_id, category, service, region, forecast_cost, confidence_low, confidence_high, method)
		SELECT
			($2::date + step.day_offset::int)::date,
			NOW(),
			provider,
			account_id,
			category,
			service,
			region,
			forecast_cost,
			GREATEST(forecast_cost - sigma, 0),
			forecast_cost + sigma,
			'trailing_7_day_average'
		FROM baseline
		CROSS JOIN generate_series(1, 7) AS step(day_offset)
	`, from, to)
	if err != nil {
		return fmt.Errorf("insert cost forecasts: %w", err)
	}
	logger.Info("dashboard forecasts recomputed", "method", "trailing_7_day_average", "horizon_days", 7)
	return nil
}

func pruneDashboardAnalytics(ctx context.Context, tx pgx.Tx, cutoff time.Time) error {
	for _, table := range []string{"daily_cost_summaries", "weekly_cost_summaries", "monthly_cost_summaries", "cost_anomalies"} {
		if _, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE period_start < $1`, table), cutoff); err != nil {
			return fmt.Errorf("prune %s: %w", table, err)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM cost_forecasts WHERE forecast_date < $1::date`, cutoff); err != nil {
		return fmt.Errorf("prune cost_forecasts: %w", err)
	}
	return nil
}

func previousSummaryWindow(interval string, from, to time.Time) (time.Time, time.Time) {
	switch interval {
	case "1 week":
		return from.AddDate(0, 0, -7), to.AddDate(0, 0, -7)
	case "1 month":
		return from.AddDate(0, -1, 0), to.AddDate(0, -1, 0)
	default:
		return from.AddDate(0, 0, -1), to.AddDate(0, 0, -1)
	}
}

func (r *Repository) DashboardCostSummaries(ctx context.Context, window string, from, to time.Time) ([]domain.DashboardCostSummary, error) {
	table := dashboardSummaryTable(window)
	rows, err := r.db.Query(ctx, fmt.Sprintf(`
		SELECT period_start, period_end, provider, account_id, compartment_id, compartment_name, billing_scope_type, billing_scope_id, billing_scope_name, record_type, service, category, region,
		       total_cost::double precision, previous_period_cost::double precision,
		       absolute_change::double precision, percentage_change::double precision, updated_at
		FROM %s
		WHERE period_start >= $1 AND period_start < $2
		ORDER BY period_start ASC, provider, account_id, billing_scope_name, record_type, service, category, region
	`, table), from.UTC(), to.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.DashboardCostSummary, 0)
	for rows.Next() {
		var row domain.DashboardCostSummary
		if err := rows.Scan(&row.PeriodStart, &row.PeriodEnd, &row.Provider, &row.AccountID, &row.CompartmentID, &row.CompartmentName, &row.BillingScopeType, &row.BillingScopeID, &row.BillingScopeName, &row.RecordType, &row.Service, &row.Category, &row.Region, &row.TotalCost, &row.PreviousPeriodCost, &row.AbsoluteChange, &row.PercentageChange, &row.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Repository) DashboardAnomalies(ctx context.Context, from, to time.Time, severity string) ([]domain.DashboardAnomaly, error) {
	args := []any{from.UTC(), to.UTC()}
	filter := ""
	if severity = strings.TrimSpace(strings.ToLower(severity)); severity != "" {
		filter = " AND severity = $3"
		args = append(args, severity)
	}
	rows, err := r.db.Query(ctx, `
		SELECT detected_at, period_start, provider, account_id, category, service, region,
		       observed_cost::double precision, expected_cost::double precision,
		       absolute_delta::double precision, percentage_delta::double precision,
		       severity, detection_method, explanation, created_at
		FROM cost_anomalies
		WHERE period_start >= $1 AND period_start < $2`+filter+`
		ORDER BY period_start DESC, ABS(absolute_delta) DESC
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.DashboardAnomaly, 0)
	for rows.Next() {
		var row domain.DashboardAnomaly
		if err := rows.Scan(&row.DetectedAt, &row.PeriodStart, &row.Provider, &row.AccountID, &row.Category, &row.Service, &row.Region, &row.ObservedCost, &row.ExpectedCost, &row.AbsoluteDelta, &row.PercentageDelta, &row.Severity, &row.DetectionMethod, &row.Explanation, &row.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Repository) DashboardOverview(ctx context.Context, currentFrom, currentTo, previousFrom, previousTo time.Time) (domain.DashboardOverview, error) {
	var out domain.DashboardOverview
	err := r.db.QueryRow(ctx, `
		WITH current_spend AS (
			SELECT COALESCE(SUM(total_cost), 0)::double precision AS total
			FROM daily_cost_summaries d
			WHERE d.period_start >= $1 AND d.period_start < $2
			  AND (
				d.provider <> 'aws'
				OR d.record_type = 'cur_line_item'
				OR (
					d.record_type = 'linked_account_service'
					AND NOT EXISTS (
						SELECT 1
						FROM daily_cost_summaries aws_cur
						WHERE aws_cur.provider = 'aws'
						  AND aws_cur.record_type = 'cur_line_item'
						  AND aws_cur.period_start = d.period_start
					)
				)
			  )
		),
		previous_spend AS (
			SELECT COALESCE(SUM(total_cost), 0)::double precision AS total
			FROM daily_cost_summaries d
			WHERE d.period_start >= $3 AND d.period_start < $4
			  AND (
				d.provider <> 'aws'
				OR d.record_type = 'cur_line_item'
				OR (
					d.record_type = 'linked_account_service'
					AND NOT EXISTS (
						SELECT 1
						FROM daily_cost_summaries aws_cur
						WHERE aws_cur.provider = 'aws'
						  AND aws_cur.record_type = 'cur_line_item'
						  AND aws_cur.period_start = d.period_start
					)
				)
			  )
		),
		anomalies AS (
			SELECT COUNT(*)::bigint AS total
			FROM cost_anomalies
			WHERE period_start >= $1 AND period_start < $2
		),
		latest_ingestion AS (
			SELECT COALESCE(
				(SELECT MAX(processed_at) FROM processed_reports WHERE status = 'processed'),
				(SELECT MAX(last_successful_ingestion_at) FROM ingestion_checkpoints),
				'epoch'::timestamptz
			) AS at
		)
		SELECT
			current_spend.total,
			previous_spend.total,
			CASE
				WHEN previous_spend.total > 0 THEN ((current_spend.total - previous_spend.total) / previous_spend.total) * 100
				WHEN current_spend.total > 0 THEN 100
				ELSE 0
			END::double precision,
			anomalies.total,
			latest_ingestion.at
		FROM current_spend, previous_spend, anomalies, latest_ingestion
	`, currentFrom.UTC(), currentTo.UTC(), previousFrom.UTC(), previousTo.UTC()).Scan(&out.Current30DaySpend, &out.Previous30DaySpend, &out.PercentageChange, &out.AnomalyCount, &out.LatestIngestionAt)
	return out, err
}

func (r *Repository) LatestIngestionStatus(ctx context.Context) (domain.IngestionStatusSummary, error) {
	rows, err := r.db.Query(ctx, `
		WITH providers AS (
			SELECT provider FROM ingestion_checkpoints
			UNION
			SELECT provider FROM processed_reports
		),
		report_totals AS (
			SELECT provider,
			       MAX(processed_at) AS latest_report_processed_at,
			       COUNT(*)::bigint AS files_processed,
			       COALESCE(SUM(record_count), 0)::bigint AS records_processed
			FROM processed_reports
			WHERE status = 'processed'
			GROUP BY provider
		),
		latest_report AS (
			SELECT DISTINCT ON (provider)
			       provider,
			       status,
			       error_message
			FROM processed_reports
			ORDER BY provider, processed_at DESC
		)
		SELECT
			p.provider,
			COALESCE(c.last_successful_ingestion_at, 'epoch'::timestamptz),
			COALESCE(c.updated_at, 'epoch'::timestamptz),
			COALESCE(t.latest_report_processed_at, 'epoch'::timestamptz),
			COALESCE(t.files_processed, 0)::bigint,
			COALESCE(t.records_processed, 0)::bigint,
			COALESCE(l.status, ''),
			COALESCE(l.error_message, '')
		FROM providers p
		LEFT JOIN ingestion_checkpoints c ON c.provider = p.provider
		LEFT JOIN report_totals t ON t.provider = p.provider
		LEFT JOIN latest_report l ON l.provider = p.provider
		ORDER BY p.provider
	`)
	if err != nil {
		return domain.IngestionStatusSummary{}, err
	}
	defer rows.Close()

	summary := domain.IngestionStatusSummary{Providers: []domain.ProviderIngestionStatus{}}
	for rows.Next() {
		var row domain.ProviderIngestionStatus
		if err := rows.Scan(&row.Provider, &row.LastSuccessfulIngestionAt, &row.CheckpointUpdatedAt, &row.LatestReportProcessedAt, &row.FilesProcessed, &row.RecordsProcessed, &row.LastStatus, &row.LastError); err != nil {
			return summary, err
		}
		if row.LastSuccessfulIngestionAt.After(summary.LatestIngestionAt) {
			summary.LatestIngestionAt = row.LastSuccessfulIngestionAt
		}
		if row.LatestReportProcessedAt.After(summary.LatestIngestionAt) {
			summary.LatestIngestionAt = row.LatestReportProcessedAt
		}
		summary.Providers = append(summary.Providers, row)
	}
	return summary, rows.Err()
}

func dashboardSummaryTable(window string) string {
	switch window {
	case "weekly":
		return "weekly_cost_summaries"
	case "monthly":
		return "monthly_cost_summaries"
	default:
		return "daily_cost_summaries"
	}
}
