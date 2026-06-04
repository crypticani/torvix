package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Migrator struct {
	pool             *pgxpool.Pool
	dir              string
	logger           *slog.Logger
	progressInterval time.Duration
}

const nonTransactionalMigrationMarker = "-- torvix:nontransactional"

func NewMigrator(pool *pgxpool.Pool, dir string) *Migrator {
	return NewMigratorWithLogger(pool, dir, slog.Default())
}

func NewMigratorWithLogger(pool *pgxpool.Pool, dir string, logger *slog.Logger) *Migrator {
	if logger == nil {
		logger = slog.Default()
	}
	return &Migrator{
		pool:             pool,
		dir:              dir,
		logger:           logger,
		progressInterval: 30 * time.Second,
	}
}

func (m *Migrator) Run(ctx context.Context) error {
	if _, err := m.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		files = append(files, entry.Name())
	}
	sort.Strings(files)

	for _, name := range files {
		var exists bool
		if err := m.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, name).Scan(&exists); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if exists {
			m.log().Info("migration already applied", "migration", name)
			continue
		}

		sqlBytes, err := os.ReadFile(filepath.Join(m.dir, name))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		sqlText := string(sqlBytes)
		mode := "transactional"

		if strings.Contains(sqlText, nonTransactionalMigrationMarker) {
			mode = "non_transactional"
			m.log().Info("migration applying", "migration", name, "mode", mode, "bytes", len(sqlBytes))
			if err := m.runWithProgress(ctx, name, mode, func() error {
				_, err := m.pool.Exec(ctx, sqlText)
				return err
			}); err != nil {
				return fmt.Errorf("apply migration %s: %w", name, err)
			}
			if _, err := m.pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ($1) ON CONFLICT (version) DO NOTHING`, name); err != nil {
				return fmt.Errorf("record migration %s: %w", name, err)
			}
			m.log().Info("migration applied", "migration", name, "mode", mode)
			continue
		}

		m.log().Info("migration applying", "migration", name, "mode", mode, "bytes", len(sqlBytes))
		tx, err := m.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", name, err)
		}
		if err := m.runWithProgress(ctx, name, mode, func() error {
			_, err := tx.Exec(ctx, sqlText)
			return err
		}); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
		m.log().Info("migration applied", "migration", name, "mode", mode)
	}

	return nil
}

func (m *Migrator) runWithProgress(ctx context.Context, name, mode string, fn func() error) error {
	started := time.Now()
	logger := m.log()
	logger.Info("migration SQL started", "migration", name, "mode", mode)

	interval := m.progressInterval
	done := make(chan struct{})
	var wg sync.WaitGroup
	if interval > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					logger.Info("migration SQL still running", "migration", name, "mode", mode, "duration", time.Since(started).String())
				case <-done:
					return
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	err := fn()
	close(done)
	wg.Wait()

	duration := time.Since(started).String()
	if err != nil {
		logger.Error("migration SQL failed", "migration", name, "mode", mode, "duration", duration, "error", err)
		return err
	}
	logger.Info("migration SQL completed", "migration", name, "mode", mode, "duration", duration)
	return nil
}

func (m *Migrator) log() *slog.Logger {
	if m.logger != nil {
		return m.logger
	}
	return slog.Default()
}
