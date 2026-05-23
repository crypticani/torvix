package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadHTTPAddressDefault(t *testing.T) {
	cfg := loadTestConfig(t, "log_level: debug\n")

	if cfg.HTTP.Address != ":8080" {
		t.Fatalf("expected default http address :8080, got %q", cfg.HTTP.Address)
	}
}

func TestLoadHTTPAddressFromConfig(t *testing.T) {
	cfg := loadTestConfig(t, "http:\n  address: \":18080\"\n")

	if cfg.HTTP.Address != ":18080" {
		t.Fatalf("expected config http address :18080, got %q", cfg.HTTP.Address)
	}
}

func TestLoadHTTPAddressEnvOverride(t *testing.T) {
	t.Setenv(EnvHTTPAddress, "0.0.0.0:19090")
	t.Setenv(EnvHTTPPort, "18080")

	cfg := loadTestConfig(t, "http:\n  address: \":8080\"\n")

	if cfg.HTTP.Address != "0.0.0.0:19090" {
		t.Fatalf("expected %s override, got %q", EnvHTTPAddress, cfg.HTTP.Address)
	}
}

func TestIngestionDefaultsUseNinetyDayOperationalHorizon(t *testing.T) {
	cfg := loadTestConfig(t, "log_level: debug\n")

	if cfg.Ingestion.LookbackDays != 30 {
		t.Fatalf("expected default lookback_days 30, got %d", cfg.Ingestion.LookbackDays)
	}
	if cfg.Ingestion.RetentionDays != 90 {
		t.Fatalf("expected default retention_days 90, got %d", cfg.Ingestion.RetentionDays)
	}
	if cfg.Ingestion.CompressionAfterDays != 7 {
		t.Fatalf("expected default compression_after_days 7, got %d", cfg.Ingestion.CompressionAfterDays)
	}
	if cfg.Ingestion.MaxZeroYieldFiles != 25 {
		t.Fatalf("expected default max_zero_yield_files 25, got %d", cfg.Ingestion.MaxZeroYieldFiles)
	}
}

func TestSchedulerDefaultsToDailyIngestion(t *testing.T) {
	cfg := loadTestConfig(t, "scheduler:\n  enabled: true\n")

	if cfg.Scheduler.IngestInterval != "24h" {
		t.Fatalf("expected default scheduler ingest_interval 24h, got %q", cfg.Scheduler.IngestInterval)
	}
}

func TestProviderInheritsMaxZeroYieldFilesDefault(t *testing.T) {
	cfg := loadTestConfig(t, "ingestion:\n  max_zero_yield_files: 17\n")
	provider := cfg.Providers.OCI.WithIngestionDefaults(cfg.Ingestion)

	if provider.MaxZeroYieldFiles != 17 {
		t.Fatalf("expected provider max_zero_yield_files 17, got %d", provider.MaxZeroYieldFiles)
	}
	if provider.IngestionLimits().MaxZeroYieldFiles != 17 {
		t.Fatalf("expected provider ingestion limit max_zero_yield_files 17, got %d", provider.IngestionLimits().MaxZeroYieldFiles)
	}
}

func TestLoadHTTPPortEnvOverride(t *testing.T) {
	t.Setenv(EnvHTTPPort, "18080")

	cfg := loadTestConfig(t, "http:\n  address: \":8080\"\n")

	if cfg.HTTP.Address != ":18080" {
		t.Fatalf("expected %s override to :18080, got %q", EnvHTTPPort, cfg.HTTP.Address)
	}
}

func loadTestConfig(t *testing.T, content string) Config {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}
