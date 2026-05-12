package postgres

import (
	"context"
	"encoding/json"
	"fmt"
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
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	const insertSQL = `
		INSERT INTO cost_records
		(timestamp, cloud_provider, account_id, service, category, resource_id, region, usage_quantity, usage_unit, cost, currency, tags, raw_json, source_object, meter)
		VALUES
		($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13::jsonb, $14, $15)
	`
	for _, record := range records {
		tags, _ := json.Marshal(record.Tags)
		raw, _ := json.Marshal(defaultRawJSON(record))
		if _, err = tx.Exec(ctx, insertSQL,
			record.Timestamp,
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
		); err != nil {
			return fmt.Errorf("insert cost record: %w", err)
		}
	}

	if _, err = tx.Exec(ctx, `
		INSERT INTO processed_report_files
		(provider, bucket, object_name, etag, last_modified, processed_at, record_count, status, error_message)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (provider, bucket, object_name, etag)
		DO UPDATE SET
			last_modified = EXCLUDED.last_modified,
			processed_at = EXCLUDED.processed_at,
			record_count = EXCLUDED.record_count,
			status = EXCLUDED.status,
			error_message = EXCLUDED.error_message
	`, file.Provider, file.Bucket, file.ObjectName, file.ETag, file.LastModified, file.ProcessedAt, file.RecordCount, file.Status, file.ErrorMessage); err != nil {
		return fmt.Errorf("mark processed report file: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *Repository) AggregateCosts(ctx context.Context, from, to time.Time, window string) ([]domain.AggregatedCost, error) {
	view := aggregateView(window)
	query := fmt.Sprintf(`
		SELECT bucket, cloud_provider, account_id, service, total_cost::double precision
		FROM %s
		WHERE bucket >= $1 AND bucket <= $2
		ORDER BY bucket ASC, cloud_provider, account_id, service
	`, view)
	rows, err := r.db.Query(ctx, query, from.UTC(), to.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.AggregatedCost
	for rows.Next() {
		var rec domain.AggregatedCost
		if err := rows.Scan(&rec.WindowStart, &rec.Provider, &rec.AccountID, &rec.Service, &rec.TotalCost); err != nil {
			return nil, err
		}
		rec.WindowEnd = aggregateWindowEnd(rec.WindowStart, window)
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (r *Repository) DetectAnomalies(ctx context.Context, from, to time.Time) ([]domain.Anomaly, error) {
	rows, err := r.db.Query(ctx, `
		WITH daily AS (
			SELECT bucket, cloud_provider, account_id, service, total_cost::double precision AS total_cost
			FROM cost_summary_daily
			WHERE bucket >= $1 - INTERVAL '7 days' AND bucket <= $2
		),
		series AS (
			SELECT
				bucket,
				cloud_provider,
				account_id,
				service,
				total_cost,
				AVG(total_cost) OVER w AS baseline,
				STDDEV_POP(total_cost) OVER w AS stddev
			FROM daily
			WINDOW w AS (
				PARTITION BY cloud_provider, account_id, service
				ORDER BY bucket
				ROWS BETWEEN 7 PRECEDING AND 1 PRECEDING
			)
		)
		SELECT
			bucket,
			cloud_provider,
			account_id,
			service,
			baseline,
			total_cost,
			CASE WHEN COALESCE(stddev, 0) > 0 THEN (total_cost - baseline) / stddev ELSE 0 END AS z_score,
			CASE WHEN COALESCE(baseline, 0) > 0 THEN ((total_cost - baseline) / baseline) * 100 ELSE 0 END AS percent_deviation,
			total_cost - baseline AS moving_average_delta
		FROM series
		WHERE bucket >= $1
		  AND bucket <= $2
		  AND baseline IS NOT NULL
		  AND (
			ABS(CASE WHEN COALESCE(stddev, 0) > 0 THEN (total_cost - baseline) / stddev ELSE 0 END) >= 2
			OR ABS(CASE WHEN COALESCE(baseline, 0) > 0 THEN ((total_cost - baseline) / baseline) * 100 ELSE 0 END) >= 30
			OR ABS(total_cost - baseline) >= baseline * 0.25
		  )
		ORDER BY bucket DESC
	`, from.UTC(), to.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Anomaly
	for rows.Next() {
		var a domain.Anomaly
		if err := rows.Scan(&a.Date, &a.Provider, &a.AccountID, &a.Service, &a.Baseline, &a.Actual, &a.ZScore, &a.PercentDeviation, &a.MovingAverageDelta); err != nil {
			return nil, err
		}
		a.Severity = anomalySeverity(a.ZScore, a.PercentDeviation)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repository) ForecastCosts(ctx context.Context, from, to time.Time, horizon int) ([]domain.ForecastPoint, error) {
	rows, err := r.db.Query(ctx, `
		WITH ranked AS (
			SELECT
				bucket,
				cloud_provider,
				account_id,
				service,
				total_cost::double precision AS total_cost,
				ROW_NUMBER() OVER (PARTITION BY cloud_provider, account_id, service ORDER BY bucket DESC) AS rn,
				MAX(bucket) OVER (PARTITION BY cloud_provider, account_id, service) AS last_bucket
			FROM cost_summary_daily
			WHERE bucket >= $1 AND bucket <= $2
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

	var out []domain.ForecastPoint
	for rows.Next() {
		var point domain.ForecastPoint
		if err := rows.Scan(&point.Date, &point.Provider, &point.AccountID, &point.Service, &point.ForecastCost, &point.ConfidenceLow, &point.ConfidenceHigh); err != nil {
			return nil, err
		}
		out = append(out, point)
	}
	return out, rows.Err()
}

func (r *Repository) IsReportProcessed(ctx context.Context, provider domain.Provider, bucket, objectName, etag string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM processed_report_files
			WHERE provider = $1 AND bucket = $2 AND object_name = $3 AND etag = $4 AND status = 'processed'
		)
	`, provider, bucket, objectName, etag).Scan(&exists)
	return exists, err
}

func aggregateView(window string) string {
	switch window {
	case "weekly":
		return "cost_summary_weekly"
	case "monthly":
		return "cost_summary_monthly"
	default:
		return "cost_summary_daily"
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
