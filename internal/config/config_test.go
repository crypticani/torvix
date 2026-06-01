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

func TestLoggingDefaults(t *testing.T) {
	cfg := loadTestConfig(t, "{}\n")

	if cfg.Logging.Level != "info" {
		t.Fatalf("expected default logging level info, got %q", cfg.Logging.Level)
	}
	if cfg.Logging.Dir != "logs" {
		t.Fatalf("expected default logging dir logs, got %q", cfg.Logging.Dir)
	}
	if cfg.Logging.RetentionDays != 14 {
		t.Fatalf("expected default logging retention_days 14, got %d", cfg.Logging.RetentionDays)
	}
}

func TestLoggingEnvOverrides(t *testing.T) {
	t.Setenv(EnvLogLevel, "debug")
	t.Setenv(EnvLogDir, "/var/log/torvix")
	t.Setenv(EnvLogRetentionDays, "3")

	cfg := loadTestConfig(t, "log_level: error\nlogging:\n  level: warn\n  dir: /tmp/torvix\n  retention_days: 30\n")

	if cfg.Logging.Level != "debug" {
		t.Fatalf("expected %s override debug, got %q", EnvLogLevel, cfg.Logging.Level)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("expected legacy log_level mirror debug, got %q", cfg.LogLevel)
	}
	if cfg.Logging.Dir != "/var/log/torvix" {
		t.Fatalf("expected %s override /var/log/torvix, got %q", EnvLogDir, cfg.Logging.Dir)
	}
	if cfg.Logging.RetentionDays != 3 {
		t.Fatalf("expected %s override 3, got %d", EnvLogRetentionDays, cfg.Logging.RetentionDays)
	}
}

func TestAPIPortAliasOverride(t *testing.T) {
	t.Setenv(EnvAPIPort, "19090")

	cfg := loadTestConfig(t, "http:\n  address: \":8080\"\n")

	if cfg.HTTP.Address != ":19090" {
		t.Fatalf("expected %s override to :19090, got %q", EnvAPIPort, cfg.HTTP.Address)
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

func TestReportingScheduleDefaults(t *testing.T) {
	cfg := loadTestConfig(t, "{}\n")

	if cfg.Reporting.Timezone != "Asia/Kolkata" {
		t.Fatalf("expected default report timezone Asia/Kolkata, got %q", cfg.Reporting.Timezone)
	}
	if cfg.Reporting.DailyReportCron != "0 14 * * *" {
		t.Fatalf("expected daily report cron 0 14 * * *, got %q", cfg.Reporting.DailyReportCron)
	}
	if cfg.Reporting.WeeklyReportCron != "0 15 * * 1" {
		t.Fatalf("expected weekly report cron 0 15 * * 1, got %q", cfg.Reporting.WeeklyReportCron)
	}
	if !cfg.Reporting.RequireCompleteIngestion {
		t.Fatal("expected report completeness gate enabled by default")
	}
	if cfg.Reporting.DailyReportTargetLagDays != 1 {
		t.Fatalf("expected daily target lag 1, got %d", cfg.Reporting.DailyReportTargetLagDays)
	}
}

func TestReportingEnvOverridesPreferTorvixOverLegacyNames(t *testing.T) {
	t.Setenv(EnvReportTimezone, "Asia/Kolkata")
	t.Setenv(LegacyEnvReportTimezone, "UTC")
	t.Setenv(EnvDailyReportCron, "5 14 * * *")
	t.Setenv(LegacyEnvDailyReportCron, "0 */6 * * *")
	t.Setenv(EnvWeeklyReportCron, "10 15 * * 1")
	t.Setenv(LegacyEnvWeeklyReportCron, "0 12 * * 0")
	t.Setenv(EnvReportRequireCompleteIngestion, "false")
	t.Setenv(LegacyEnvReportRequireCompleteIngestion, "true")
	t.Setenv(EnvDailyReportTargetLagDays, "2")
	t.Setenv(LegacyEnvDailyReportTargetLagDays, "1")

	cfg := loadTestConfig(t, "{}\n")

	if cfg.Reporting.Timezone != "Asia/Kolkata" {
		t.Fatalf("expected %s override, got %q", EnvReportTimezone, cfg.Reporting.Timezone)
	}
	if cfg.Reporting.DailyReportCron != "5 14 * * *" {
		t.Fatalf("expected %s override, got %q", EnvDailyReportCron, cfg.Reporting.DailyReportCron)
	}
	if cfg.Reporting.WeeklyReportCron != "10 15 * * 1" {
		t.Fatalf("expected %s override, got %q", EnvWeeklyReportCron, cfg.Reporting.WeeklyReportCron)
	}
	if cfg.Reporting.RequireCompleteIngestion {
		t.Fatalf("expected %s=false override", EnvReportRequireCompleteIngestion)
	}
	if cfg.Reporting.DailyReportTargetLagDays != 2 {
		t.Fatalf("expected %s=2 override, got %d", EnvDailyReportTargetLagDays, cfg.Reporting.DailyReportTargetLagDays)
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
