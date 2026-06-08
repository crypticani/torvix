package logging

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManagerWritesSubsystemLogsToSeparateFiles(t *testing.T) {
	dir := t.TempDir()

	manager, err := NewManager(Config{
		Level:         "info",
		Dir:           dir,
		RetentionDays: 14,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer manager.Close()

	manager.Logger(SubsystemApp).Info("app started")
	manager.Logger(SubsystemHTTP).Warn("request slow", "path", "/healthz")
	if err := manager.Close(); err != nil {
		t.Fatalf("close manager: %v", err)
	}

	appLog := readLogFile(t, dir, "app.log")
	httpLog := readLogFile(t, dir, "http.log")

	if !strings.Contains(appLog, "app started") {
		t.Fatalf("expected app log to contain app message, got %q", appLog)
	}
	if strings.Contains(appLog, "request slow") {
		t.Fatalf("expected app log not to contain http message, got %q", appLog)
	}
	if !strings.Contains(httpLog, "request slow") {
		t.Fatalf("expected http log to contain http message, got %q", httpLog)
	}
}

func TestManagerFiltersByConfiguredLevel(t *testing.T) {
	dir := t.TempDir()

	manager, err := NewManager(Config{
		Level:         "warn",
		Dir:           dir,
		RetentionDays: 14,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer manager.Close()

	logger := manager.Logger(SubsystemApp)
	logger.Info("info hidden")
	logger.Error("error visible")
	if err := manager.Close(); err != nil {
		t.Fatalf("close manager: %v", err)
	}

	appLog := readLogFile(t, dir, "app.log")
	if strings.Contains(appLog, "info hidden") {
		t.Fatalf("expected info log to be filtered at warn level, got %q", appLog)
	}
	if !strings.Contains(appLog, "error visible") {
		t.Fatalf("expected error log to be written at warn level, got %q", appLog)
	}
}

func TestManagerDoesNotWriteToStdout(t *testing.T) {
	dir := t.TempDir()
	stdout := captureStdout(t, func() {
		manager, err := NewManager(Config{
			Level:         "info",
			Dir:           dir,
			RetentionDays: 14,
		})
		if err != nil {
			t.Fatalf("new manager: %v", err)
		}
		defer manager.Close()

		manager.Logger(SubsystemApp).Info("file only")
		if err := manager.Close(); err != nil {
			t.Fatalf("close manager: %v", err)
		}
	})

	if stdout != "" {
		t.Fatalf("expected no stdout log output, got %q", stdout)
	}
}

func TestManagerCanMirrorLogsToStdout(t *testing.T) {
	dir := t.TempDir()
	stdout := captureStdout(t, func() {
		manager, err := NewManager(Config{
			Level:         "info",
			Dir:           dir,
			RetentionDays: 14,
			Stdout:        true,
		})
		if err != nil {
			t.Fatalf("new manager: %v", err)
		}
		defer manager.Close()

		manager.Logger(SubsystemApp).Info("visible in docker logs")
		if err := manager.Close(); err != nil {
			t.Fatalf("close manager: %v", err)
		}
	})

	if !strings.Contains(stdout, "visible in docker logs") {
		t.Fatalf("expected stdout mirror, got %q", stdout)
	}
	if !strings.Contains(readLogFile(t, dir, "app.log"), "visible in docker logs") {
		t.Fatalf("expected stdout-mirrored message to remain in app.log")
	}
}

func TestCleanupExpiredDeletesOldLogFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	oldPath := filepath.Join(dir, "app-2026-05-01.log")
	freshPath := filepath.Join(dir, "app.log")
	nestedPath := filepath.Join(dir, "archive")

	writeFileWithModTime(t, oldPath, "old", now.AddDate(0, 0, -10))
	writeFileWithModTime(t, freshPath, "fresh", now.AddDate(0, 0, -2))
	if err := os.Mkdir(nestedPath, 0o755); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}

	if err := CleanupExpired(dir, 7, now); err != nil {
		t.Fatalf("cleanup expired: %v", err)
	}

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expected old log file to be deleted, stat err=%v", err)
	}
	if _, err := os.Stat(freshPath); err != nil {
		t.Fatalf("expected fresh log file to remain: %v", err)
	}
	if _, err := os.Stat(nestedPath); err != nil {
		t.Fatalf("expected nested directory to remain: %v", err)
	}
}

func readLogFile(t *testing.T, dir, name string) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func writeFileWithModTime(t *testing.T, path, content string, modTime time.Time) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("set mtime %s: %v", path, err)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = original
	}()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, reader); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return buf.String()
}
