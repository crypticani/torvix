package clickhouse

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/crypticani/cloudpulse/internal/domain"
	"github.com/crypticani/cloudpulse/internal/ports/storage"
)

var _ storage.Repository = (*Repository)(nil)

type Repository struct {
	db *sql.DB
}

func New(dsn string) (*Repository, error) {
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	return &Repository{db: db}, nil
}

func (r *Repository) Close() error { return r.db.Close() }

func (r *Repository) InsertCanonical(ctx context.Context, records []domain.CanonicalCostRecord) error {
	if len(records) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO billing_canonical
		(date, provider, account_id, service, region, resource_id, currency, cost, usage_amount, usage_unit, tags_json, source_object)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, rec := range records {
		tags, _ := json.Marshal(rec.Tags)
		if _, err := stmt.ExecContext(ctx, rec.Date, rec.Provider, rec.AccountID, rec.Service, rec.Region, rec.ResourceID, rec.Currency, rec.Cost, rec.UsageAmount, rec.UsageUnit, string(tags), rec.SourceObject); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) GetDailyCosts(ctx context.Context, from, to time.Time) ([]domain.AggregatedCost, error) {
	return r.queryAggregates(ctx, from, to, false)
}

func (r *Repository) GetDailyCostsByService(ctx context.Context, from, to time.Time) ([]domain.AggregatedCost, error) {
	return r.queryAggregates(ctx, from, to, true)
}

func (r *Repository) queryAggregates(ctx context.Context, from, to time.Time, byService bool) ([]domain.AggregatedCost, error) {
	query := `
		SELECT date, date, provider, account_id, '' AS service, sum(cost) AS total_cost
		FROM billing_canonical
		WHERE date >= ? AND date <= ?
		GROUP BY date, provider, account_id
		ORDER BY date ASC
	`
	if byService {
		query = `
			SELECT date, date, provider, account_id, service, sum(cost) AS total_cost
			FROM billing_canonical
			WHERE date >= ? AND date <= ?
			GROUP BY date, provider, account_id, service
			ORDER BY date ASC
		`
	}
	rows, err := r.db.QueryContext(ctx, query, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.AggregatedCost
	for rows.Next() {
		var rec domain.AggregatedCost
		if err := rows.Scan(&rec.WindowStart, &rec.WindowEnd, &rec.Provider, &rec.AccountID, &rec.Service, &rec.TotalCost); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (r *Repository) SaveAnomalies(ctx context.Context, anomalies []domain.Anomaly) error {
	if len(anomalies) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO anomalies
		(date, provider, account_id, service, baseline, actual, z_score, percent_deviation, moving_average_delta, severity)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, a := range anomalies {
		if _, err := stmt.ExecContext(ctx, a.Date, a.Provider, a.AccountID, a.Service, a.Baseline, a.Actual, a.ZScore, a.PercentDeviation, a.MovingAverageDelta, a.Severity); err != nil {
			tx.Rollback()
			return fmt.Errorf("insert anomaly: %w", err)
		}
	}
	return tx.Commit()
}

func (r *Repository) GetAnomalies(ctx context.Context, from, to time.Time) ([]domain.Anomaly, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT date, provider, account_id, service, baseline, actual, z_score, percent_deviation, moving_average_delta, severity
		FROM anomalies
		WHERE date >= ? AND date <= ?
		ORDER BY date DESC
	`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Anomaly
	for rows.Next() {
		var a domain.Anomaly
		if err := rows.Scan(&a.Date, &a.Provider, &a.AccountID, &a.Service, &a.Baseline, &a.Actual, &a.ZScore, &a.PercentDeviation, &a.MovingAverageDelta, &a.Severity); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
