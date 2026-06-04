package postgres

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMigratorLogsProgressForLongRunningMigrationSQL(t *testing.T) {
	var logs lockedStringWriter
	migrator := &Migrator{
		logger:           slog.New(slog.NewJSONHandler(&logs, nil)),
		progressInterval: time.Millisecond,
	}

	err := migrator.runWithProgress(context.Background(), "011_provider_agnostic_cost_dimensions.sql", "non_transactional", func() error {
		time.Sleep(20 * time.Millisecond)
		return nil
	})
	if err != nil {
		t.Fatalf("runWithProgress() error = %v", err)
	}

	out := logs.String()
	for _, expected := range []string{
		"migration SQL started",
		"migration SQL still running",
		"migration SQL completed",
		"011_provider_agnostic_cost_dimensions.sql",
		"non_transactional",
		"duration",
	} {
		if !strings.Contains(out, expected) {
			t.Fatalf("expected progress logs to contain %q, got %s", expected, out)
		}
	}
}

func TestMigratorLogsFailedMigrationSQL(t *testing.T) {
	var logs lockedStringWriter
	migrator := &Migrator{
		logger:           slog.New(slog.NewJSONHandler(&logs, nil)),
		progressInterval: 0,
	}
	expectedErr := errors.New("boom")

	err := migrator.runWithProgress(context.Background(), "011_provider_agnostic_cost_dimensions.sql", "transactional", func() error {
		return expectedErr
	})
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected %v, got %v", expectedErr, err)
	}

	out := logs.String()
	for _, expected := range []string{
		"migration SQL started",
		"migration SQL failed",
		"011_provider_agnostic_cost_dimensions.sql",
		"transactional",
		"boom",
	} {
		if !strings.Contains(out, expected) {
			t.Fatalf("expected failure logs to contain %q, got %s", expected, out)
		}
	}
	if strings.Contains(out, "migration SQL completed") {
		t.Fatalf("expected failure logs not to contain completion, got %s", out)
	}
}

type lockedStringWriter struct {
	mu sync.Mutex
	b  strings.Builder
}

func (w *lockedStringWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func (w *lockedStringWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.String()
}
