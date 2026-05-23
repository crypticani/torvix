package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/crypticani/cloudpulse/internal/domain"
	"github.com/crypticani/cloudpulse/internal/ports/storage"
)

var _ storage.Repository = (*Repository)(nil)

type Repository struct {
	db   DB
	pool *pgxpool.Pool
}

type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func New(ctx context.Context, dsn string, maxConns, minConns int32) (*Repository, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres config: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	if minConns > 0 {
		cfg.MinConns = minConns
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Repository{db: pool, pool: pool}, nil
}

func NewWithDB(db DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Close() {
	if r.pool != nil {
		r.pool.Close()
	}
}

func (r *Repository) Pool() *pgxpool.Pool {
	return r.pool
}

func (r *Repository) StoreIngestedBatch(ctx context.Context, file domain.ProcessedReportFile, records []domain.CanonicalCostRecord) error {
	started := time.Now()
	if err := r.ensureCostRecordChunksWritableForRecords(ctx, records); err != nil {
		return err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	if err = storeCostRecordsTx(ctx, tx, records); err != nil {
		return err
	}
	if err = markReportProcessedTx(ctx, tx, file); err != nil {
		return fmt.Errorf("mark processed report file: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return err
	}
	slog.Info("ingested batch committed", "provider", file.Provider, "object", file.ObjectName, "records_inserted", len(records), "duration", time.Since(started).String())
	return nil
}

func (r *Repository) StoreCostRecords(ctx context.Context, records []domain.CanonicalCostRecord) error {
	if len(records) == 0 {
		return nil
	}
	started := time.Now()
	if err := r.ensureCostRecordChunksWritableForRecords(ctx, records); err != nil {
		return err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if err = storeCostRecordsTx(ctx, tx, records); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	slog.Info("cost records batch committed", "records_inserted", len(records), "duration", time.Since(started).String())
	return nil
}

func (r *Repository) DeleteCostRecordsForSource(ctx context.Context, provider domain.Provider, sourceObject string) error {
	if sourceObject == "" {
		return nil
	}
	from, to, ok, err := r.costRecordSourceRange(ctx, provider, sourceObject)
	if err != nil {
		return err
	}
	if ok {
		if err := r.ensureCostRecordChunksWritable(ctx, from, to); err != nil {
			return err
		}
	}
	_, err = r.db.Exec(ctx, `
		DELETE FROM cost_records
		WHERE cloud_provider = $1 AND source_object = $2
	`, provider, sourceObject)
	return err
}

func (r *Repository) MarkReportProcessed(ctx context.Context, file domain.ProcessedReportFile) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if err = markReportProcessedTx(ctx, tx, file); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) LastIngestionCheckpoint(ctx context.Context, provider domain.Provider) (time.Time, error) {
	var checkpoint time.Time
	err := r.db.QueryRow(ctx, `
		SELECT COALESCE(MAX(last_successful_ingestion_at), 'epoch'::timestamptz)
		FROM ingestion_checkpoints
		WHERE provider = $1
	`, provider).Scan(&checkpoint)
	return checkpoint.UTC(), err
}

func (r *Repository) MarkIngestionCheckpoint(ctx context.Context, provider domain.Provider, checkpoint time.Time) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO ingestion_checkpoints (provider, last_successful_ingestion_at, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (provider)
		DO UPDATE SET
			last_successful_ingestion_at = GREATEST(ingestion_checkpoints.last_successful_ingestion_at, EXCLUDED.last_successful_ingestion_at),
			updated_at = NOW()
	`, provider, checkpoint.UTC())
	return err
}

func (r *Repository) ApplyDataLifecyclePolicies(ctx context.Context, retentionDays, compressionAfterDays int) error {
	if retentionDays <= 0 {
		retentionDays = 90
	}
	if compressionAfterDays <= 0 {
		compressionAfterDays = 7
	}
	if _, err := r.db.Exec(ctx, `SELECT remove_retention_policy('cost_records', if_exists => TRUE)`); err != nil {
		return fmt.Errorf("remove retention policy: %w", err)
	}
	if _, err := r.db.Exec(ctx, `SELECT add_retention_policy('cost_records', make_interval(days => $1), if_not_exists => TRUE)`, retentionDays); err != nil {
		return fmt.Errorf("add retention policy: %w", err)
	}
	if _, err := r.db.Exec(ctx, `SELECT remove_compression_policy('cost_records', if_exists => TRUE)`); err != nil {
		return fmt.Errorf("remove compression policy: %w", err)
	}
	if _, err := r.db.Exec(ctx, `SELECT add_compression_policy('cost_records', make_interval(days => $1), if_not_exists => TRUE)`, compressionAfterDays); err != nil {
		return fmt.Errorf("add compression policy: %w", err)
	}
	slog.Info("data lifecycle policies applied", "retention_days", retentionDays, "compression_after_days", compressionAfterDays)
	return nil
}

func (r *Repository) RunDataLifecycleMaintenance(ctx context.Context, retentionDays, compressionAfterDays int) (domain.DataLifecycleMaintenance, error) {
	if retentionDays <= 0 {
		retentionDays = 90
	}
	if compressionAfterDays <= 0 {
		compressionAfterDays = 7
	}
	retentionCutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)
	var result domain.DataLifecycleMaintenance
	tag, err := r.db.Exec(ctx, `
		DELETE FROM cost_records
		WHERE "timestamp" < $1
	`, retentionCutoff)
	if err != nil {
		return result, fmt.Errorf("prune old cost records: %w", err)
	}
	result.RecordsDeleted = tag.RowsAffected()
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return result, fmt.Errorf("begin dashboard analytics prune: %w", err)
	}
	if err = pruneDashboardAnalytics(ctx, tx, retentionCutoff); err != nil {
		_ = tx.Rollback(ctx)
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, fmt.Errorf("commit dashboard analytics prune: %w", err)
	}

	err = r.db.QueryRow(ctx, `
		SELECT COUNT(*)::bigint
		FROM (
			SELECT compress_chunk(chunk, if_not_compressed => TRUE)
			FROM show_chunks('cost_records', older_than => make_interval(days => $1)) AS chunk
		) compressed
	`, compressionAfterDays).Scan(&result.CompressedChunks)
	if err != nil {
		return result, fmt.Errorf("compress old cost chunks: %w", err)
	}
	slog.Info("data lifecycle maintenance completed", "records_deleted", result.RecordsDeleted, "compressed_chunks", result.CompressedChunks, "retention_days", retentionDays, "compression_after_days", compressionAfterDays)
	return result, nil
}

func (r *Repository) ensureCostRecordChunksWritableForRecords(ctx context.Context, records []domain.CanonicalCostRecord) error {
	from, to, ok := costRecordRange(records)
	if !ok {
		return nil
	}
	return r.ensureCostRecordChunksWritable(ctx, from, to)
}

func costRecordRange(records []domain.CanonicalCostRecord) (time.Time, time.Time, bool) {
	var from, to time.Time
	for _, record := range records {
		ts := record.Timestamp.UTC()
		if ts.IsZero() {
			continue
		}
		if from.IsZero() || ts.Before(from) {
			from = ts
		}
		if to.IsZero() || ts.After(to) {
			to = ts
		}
	}
	if from.IsZero() || to.IsZero() {
		return time.Time{}, time.Time{}, false
	}
	return from, to, true
}

func (r *Repository) costRecordSourceRange(ctx context.Context, provider domain.Provider, sourceObject string) (time.Time, time.Time, bool, error) {
	var from, to sql.NullTime
	err := r.db.QueryRow(ctx, `
		SELECT MIN("timestamp"), MAX("timestamp")
		FROM cost_records
		WHERE cloud_provider = $1 AND source_object = $2
	`, provider, sourceObject).Scan(&from, &to)
	if err != nil {
		return time.Time{}, time.Time{}, false, fmt.Errorf("read source record range: %w", err)
	}
	if !from.Valid || !to.Valid {
		return time.Time{}, time.Time{}, false, nil
	}
	return from.Time.UTC(), to.Time.UTC(), true, nil
}

func (r *Repository) ensureCostRecordChunksWritable(ctx context.Context, from, to time.Time) error {
	if from.IsZero() || to.IsZero() {
		return nil
	}
	from = from.UTC().Add(-time.Nanosecond)
	to = to.UTC().Add(time.Nanosecond)
	if to.Before(from) {
		from, to = to, from
	}
	var chunks int64
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*)::bigint
		FROM (
			SELECT decompress_chunk(chunk, if_compressed => TRUE)
			FROM show_chunks('cost_records', newer_than => $1::timestamptz, older_than => $2::timestamptz) AS chunk
		) decompressed
	`, from, to).Scan(&chunks)
	if err != nil {
		return fmt.Errorf("decompress cost record chunks: %w", err)
	}
	if chunks > 0 {
		slog.Info("cost record chunks made writable", "from", from, "to", to, "chunks", chunks)
	}
	return nil
}

func storeCostRecordsTx(ctx context.Context, tx pgx.Tx, records []domain.CanonicalCostRecord) error {
	if len(records) == 0 {
		return nil
	}
	const insertSQL = `
		INSERT INTO cost_records
		(timestamp, cloud_provider, account_id, service, category, resource_id, region, usage_quantity, usage_unit, cost, currency, tags, raw_json, source_object, meter)
		VALUES
		($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13::jsonb, $14, $15)
	`
	batch := &pgx.Batch{}
	for _, record := range records {
		tags, _ := json.Marshal(record.Tags)
		raw, _ := json.Marshal(defaultRawJSON(record))
		batch.Queue(insertSQL,
			record.Timestamp.UTC(),
			record.Provider,
			record.AccountID,
			record.Service,
			record.Category,
			record.ResourceID,
			record.Region,
			record.UsageAmount,
			record.UsageUnit,
			record.Cost,
			record.Currency,
			string(tags),
			string(raw),
			record.SourceObject,
			record.Meter,
		)
	}
	results := tx.SendBatch(ctx, batch)
	for range records {
		if _, err := results.Exec(); err != nil {
			_ = results.Close()
			return fmt.Errorf("insert cost record: %w", err)
		}
	}
	if err := results.Close(); err != nil {
		return fmt.Errorf("close cost record batch: %w", err)
	}
	return nil
}

func markReportProcessedTx(ctx context.Context, tx pgx.Tx, file domain.ProcessedReportFile) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO processed_reports
		(provider, bucket, object_name, etag, last_modified, processed_at, record_count, status, error_message)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (provider, bucket, object_name, etag)
		DO UPDATE SET
			last_modified = EXCLUDED.last_modified,
			processed_at = EXCLUDED.processed_at,
			record_count = EXCLUDED.record_count,
			status = EXCLUDED.status,
			error_message = EXCLUDED.error_message
	`, file.Provider, file.Bucket, file.ObjectName, file.ETag, file.LastModified, file.ProcessedAt, file.RecordCount, file.Status, file.ErrorMessage)
	return err
}

func (r *Repository) AggregateCosts(ctx context.Context, from, to time.Time, window string) ([]domain.AggregatedCost, error) {
	started := time.Now()
	bucket := aggregateBucket(window)
	query := fmt.Sprintf(`
		SELECT
			time_bucket(%s::interval, "timestamp") AS bucket,
			cloud_provider,
			COALESCE(account_id, '') AS account_id,
			service,
			COALESCE(SUM(cost), 0)::double precision AS total_cost
		FROM cost_records
		WHERE "timestamp" >= $1 AND "timestamp" < $2
		GROUP BY 1, 2, 3, 4
		ORDER BY 1 ASC, 2, 3, 4
	`, bucket)
	rows, err := r.db.Query(ctx, query, from.UTC(), to.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.AggregatedCost, 0)
	for rows.Next() {
		var rec domain.AggregatedCost
		if err := rows.Scan(&rec.WindowStart, &rec.Provider, &rec.AccountID, &rec.Service, &rec.TotalCost); err != nil {
			return nil, err
		}
		rec.WindowEnd = aggregateWindowEnd(rec.WindowStart, window)
		out = append(out, rec)
	}
	err = rows.Err()
	slog.Info("analytics summary query executed", "window", window, "from", from.UTC(), "to", to.UTC(), "rows", len(out), "duration", time.Since(started).String(), "error", err)
	return out, err
}

func (r *Repository) CompareCostVariance(ctx context.Context, period string, currentFrom, currentTo, previousFrom, previousTo time.Time) ([]domain.CostVariance, error) {
	started := time.Now()
	rows, err := r.db.Query(ctx, `
		WITH current_window AS (
			SELECT
				cloud_provider,
				COALESCE(account_id, '') AS account_id,
				service,
				COALESCE(tags->>'oci_compartment_id', '') AS compartment_id,
				COALESCE(NULLIF(tags->>'oci_compartment_name', ''), tags->>'oci_compartment_id', 'unknown') AS compartment_name,
				COALESCE(SUM(cost), 0)::double precision AS total_cost
			FROM cost_records
			WHERE "timestamp" >= $1 AND "timestamp" < $2
			GROUP BY 1, 2, 3, 4, 5
		),
		previous_window AS (
			SELECT
				cloud_provider,
				COALESCE(account_id, '') AS account_id,
				service,
				COALESCE(tags->>'oci_compartment_id', '') AS compartment_id,
				COALESCE(NULLIF(tags->>'oci_compartment_name', ''), tags->>'oci_compartment_id', 'unknown') AS compartment_name,
				COALESCE(SUM(cost), 0)::double precision AS total_cost
			FROM cost_records
			WHERE "timestamp" >= $3 AND "timestamp" < $4
			GROUP BY 1, 2, 3, 4, 5
		),
		joined AS (
			SELECT
				COALESCE(c.cloud_provider, p.cloud_provider) AS cloud_provider,
				COALESCE(c.account_id, p.account_id) AS account_id,
				COALESCE(c.service, p.service) AS service,
				COALESCE(c.compartment_id, p.compartment_id) AS compartment_id,
				COALESCE(c.compartment_name, p.compartment_name) AS compartment_name,
				COALESCE(c.total_cost, 0)::double precision AS current_cost,
				COALESCE(p.total_cost, 0)::double precision AS previous_cost
			FROM current_window c
			FULL OUTER JOIN previous_window p
			  ON c.cloud_provider = p.cloud_provider
			 AND c.account_id = p.account_id
			 AND c.service = p.service
			 AND c.compartment_id = p.compartment_id
			 AND c.compartment_name = p.compartment_name
		)
		SELECT
			cloud_provider,
			account_id,
			service,
			compartment_id,
			compartment_name,
			current_cost,
			previous_cost,
			current_cost - previous_cost AS delta,
			CASE
				WHEN previous_cost > 0 THEN ((current_cost - previous_cost) / previous_cost) * 100
				WHEN current_cost > 0 THEN 100
				ELSE 0
			END AS percent_change,
			CASE
				WHEN current_cost > previous_cost THEN 'increase'
				WHEN current_cost < previous_cost THEN 'decrease'
				ELSE 'flat'
			END AS direction
		FROM joined
		WHERE current_cost <> 0 OR previous_cost <> 0
		ORDER BY ABS(current_cost - previous_cost) DESC, cloud_provider, service, compartment_name
	`, currentFrom.UTC(), currentTo.UTC(), previousFrom.UTC(), previousTo.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.CostVariance, 0)
	for rows.Next() {
		rec := domain.CostVariance{
			Period:              period,
			CurrentWindowStart:  currentFrom.UTC(),
			CurrentWindowEnd:    currentTo.UTC(),
			PreviousWindowStart: previousFrom.UTC(),
			PreviousWindowEnd:   previousTo.UTC(),
		}
		if err := rows.Scan(&rec.Provider, &rec.AccountID, &rec.Service, &rec.CompartmentID, &rec.CompartmentName, &rec.CurrentCost, &rec.PreviousCost, &rec.Delta, &rec.PercentChange, &rec.Direction); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	err = rows.Err()
	slog.Info("analytics cost variance query executed", "period", period, "current_from", currentFrom.UTC(), "current_to", currentTo.UTC(), "previous_from", previousFrom.UTC(), "previous_to", previousTo.UTC(), "rows", len(out), "duration", time.Since(started).String(), "error", err)
	return out, err
}

func (r *Repository) DetectAnomalies(ctx context.Context, from, to time.Time) ([]domain.Anomaly, error) {
	started := time.Now()
	rows, err := r.db.Query(ctx, `
		WITH daily AS (
			SELECT
				time_bucket(INTERVAL '1 day', "timestamp") AS bucket,
				cloud_provider,
				COALESCE(account_id, '') AS account_id,
				COALESCE(service, 'unknown') AS service,
				COALESCE(category, 'uncategorized') AS category,
				COALESCE(tags->>'oci_compartment_id', '') AS compartment_id,
				COALESCE(NULLIF(tags->>'oci_compartment_name', ''), tags->>'oci_compartment_id', '') AS compartment_name,
				COALESCE(region, '') AS region,
				COALESCE(SUM(cost), 0)::double precision AS total_cost
			FROM cost_records
			WHERE "timestamp" >= $1::timestamptz - INTERVAL '7 days'
			  AND "timestamp" < $2
			GROUP BY 1, 2, 3, 4, 5, 6, 7, 8
		),
		series AS (
			SELECT
				bucket,
				cloud_provider,
				account_id,
				service,
				category,
				compartment_id,
				compartment_name,
				region,
				total_cost,
				AVG(total_cost) OVER w AS baseline,
				STDDEV_POP(total_cost) OVER w AS stddev
			FROM daily
			WINDOW w AS (
				PARTITION BY cloud_provider, account_id, service, category, compartment_id, compartment_name, region
				ORDER BY bucket
				ROWS BETWEEN 7 PRECEDING AND 1 PRECEDING
			)
		)
		SELECT
			bucket,
			cloud_provider,
			account_id,
			service,
			category,
			compartment_id,
			compartment_name,
			region,
			baseline,
			total_cost,
			CASE WHEN COALESCE(stddev, 0) > 0 THEN (total_cost - baseline) / stddev ELSE 0 END AS z_score,
			CASE WHEN COALESCE(baseline, 0) > 0 THEN ((total_cost - baseline) / baseline) * 100 ELSE 0 END AS percent_deviation,
			total_cost - baseline AS moving_average_delta
		FROM series
		WHERE bucket >= $1
		  AND bucket < $2
		  AND baseline IS NOT NULL
		  AND ABS(total_cost - baseline) >= 1
		  AND (
			ABS(CASE WHEN COALESCE(stddev, 0) > 0 THEN (total_cost - baseline) / stddev ELSE 0 END) >= 2
			OR (baseline > 0 AND ABS(((total_cost - baseline) / baseline) * 100) >= 30)
		  )
		ORDER BY bucket DESC
	`, from.UTC(), to.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Anomaly, 0)
	for rows.Next() {
		var a domain.Anomaly
		if err := rows.Scan(&a.Date, &a.Provider, &a.AccountID, &a.Service, &a.Category, &a.CompartmentID, &a.CompartmentName, &a.Region, &a.Baseline, &a.Actual, &a.ZScore, &a.PercentDeviation, &a.MovingAverageDelta); err != nil {
			return nil, err
		}
		a.Severity = anomalySeverity(a.ZScore, a.PercentDeviation)
		out = append(out, a)
	}
	err = rows.Err()
	slog.Info("analytics anomalies query executed", "from", from.UTC(), "to", to.UTC(), "rows", len(out), "duration", time.Since(started).String(), "error", err)
	return out, err
}

func (r *Repository) ForecastCosts(ctx context.Context, from, to time.Time, horizon int) ([]domain.ForecastPoint, error) {
	started := time.Now()
	rows, err := r.db.Query(ctx, `
		WITH daily AS (
			SELECT
				time_bucket(INTERVAL '1 day', "timestamp") AS bucket,
				cloud_provider,
				COALESCE(account_id, '') AS account_id,
				service,
				COALESCE(SUM(cost), 0)::double precision AS total_cost
			FROM cost_records
			WHERE "timestamp" >= $1 AND "timestamp" < $2
			GROUP BY 1, 2, 3, 4
		),
		ranked AS (
			SELECT
				bucket,
				cloud_provider,
				account_id,
				service,
				total_cost::double precision AS total_cost,
				ROW_NUMBER() OVER (PARTITION BY cloud_provider, account_id, service ORDER BY bucket DESC) AS rn,
				MAX(bucket) OVER (PARTITION BY cloud_provider, account_id, service) AS last_bucket
			FROM daily
		),
		recent AS (
			SELECT
				cloud_provider,
				account_id,
				service,
				last_bucket,
				AVG(total_cost) AS forecast_cost,
				COALESCE(STDDEV_POP(total_cost), 0) AS sigma
			FROM ranked
			WHERE rn <= 7
			GROUP BY cloud_provider, account_id, service, last_bucket
		)
		SELECT
			last_bucket + make_interval(days => step.day_offset),
			cloud_provider,
			account_id,
			service,
			forecast_cost,
			GREATEST(forecast_cost - sigma, 0),
			forecast_cost + sigma
		FROM recent
		CROSS JOIN generate_series(1, $3) AS step(day_offset)
		ORDER BY 1 ASC, 2, 3, 4
	`, from.UTC(), to.UTC(), horizon)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.ForecastPoint, 0)
	for rows.Next() {
		var point domain.ForecastPoint
		if err := rows.Scan(&point.Date, &point.Provider, &point.AccountID, &point.Service, &point.ForecastCost, &point.ConfidenceLow, &point.ConfidenceHigh); err != nil {
			return nil, err
		}
		out = append(out, point)
	}
	err = rows.Err()
	slog.Info("analytics forecast query executed", "from", from.UTC(), "to", to.UTC(), "horizon", horizon, "rows", len(out), "duration", time.Since(started).String(), "error", err)
	return out, err
}

func (r *Repository) IsReportProcessed(ctx context.Context, provider domain.Provider, bucket, objectName, etag string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM processed_reports
			WHERE provider = $1 AND bucket = $2 AND object_name = $3 AND etag = $4 AND status = 'processed'
		)
	`, provider, bucket, objectName, etag).Scan(&exists)
	return exists, err
}

func (r *Repository) RefreshAggregates(ctx context.Context, from, to time.Time) error {
	views := []struct {
		name  string
		align func(time.Time, time.Time) (time.Time, time.Time)
	}{
		{name: "cost_summary_daily", align: alignDailyRefreshWindow},
		{name: "cost_summary_weekly", align: alignWeeklyRefreshWindow},
		{name: "cost_summary_monthly", align: alignMonthlyRefreshWindow},
	}
	for _, view := range views {
		refreshFrom, refreshTo := view.align(from.UTC(), to.UTC())
		if !refreshTo.After(refreshFrom) {
			continue
		}
		if _, err := r.db.Exec(ctx,
			fmt.Sprintf("CALL refresh_continuous_aggregate('%s', $1::timestamptz, $2::timestamptz)", view.name),
			refreshFrom, refreshTo,
		); err != nil {
			return fmt.Errorf("refresh %s: %w", view.name, err)
		}
	}
	return nil
}

func alignDailyRefreshWindow(from, to time.Time) (time.Time, time.Time) {
	return dayStart(from), dayStart(to).AddDate(0, 0, 1)
}

func alignWeeklyRefreshWindow(from, to time.Time) (time.Time, time.Time) {
	return weekStart(from), weekStart(to).AddDate(0, 0, 7)
}

func alignMonthlyRefreshWindow(from, to time.Time) (time.Time, time.Time) {
	return monthStart(from), monthStart(to).AddDate(0, 1, 0)
}

func dayStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func weekStart(t time.Time) time.Time {
	start := dayStart(t)
	weekday := int(start.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	return start.AddDate(0, 0, -(weekday - 1))
}

func monthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func aggregateBucket(window string) string {
	switch window {
	case "weekly":
		return "'1 week'"
	case "monthly":
		return "'1 month'"
	default:
		return "'1 day'"
	}
}

func aggregateWindowEnd(start time.Time, window string) time.Time {
	switch window {
	case "weekly":
		return start.AddDate(0, 0, 6)
	case "monthly":
		return start.AddDate(0, 1, -1)
	default:
		return start
	}
}

func defaultRawJSON(record domain.CanonicalCostRecord) map[string]any {
	if record.RawData != nil {
		return record.RawData
	}
	return map[string]any{
		"provider":      record.Provider,
		"meter":         record.Meter,
		"source_object": record.SourceObject,
	}
}

func anomalySeverity(zScore, percentDeviation float64) string {
	if math.Abs(zScore) >= 3 || math.Abs(percentDeviation) >= 50 {
		return "high"
	}
	return "medium"
}
