package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestLogStdoutEnvOverride(t *testing.T) {
	t.Setenv(EnvLogStdout, "true")

	cfg := loadTestConfig(t, "{}\n")

	if !cfg.Logging.Stdout {
		t.Fatalf("expected %s=true to enable stdout log mirroring", EnvLogStdout)
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

func TestAPIAuthConfigUsesNeutralName(t *testing.T) {
	cfg := loadTestConfig(t, "api:\n  auth:\n    enabled: true\n    bearer_token: api-secret\n")

	if !cfg.API.Auth.Enabled {
		t.Fatal("expected api auth enabled")
	}
	if cfg.API.Auth.BearerToken != "api-secret" {
		t.Fatalf("expected api bearer token, got %q", cfg.API.Auth.BearerToken)
	}
}

func TestAPIAuthEnvOverridePrefersNeutralName(t *testing.T) {
	t.Setenv(EnvAPIBearerToken, "api-secret")

	cfg := loadTestConfig(t, "{}\n")

	if !cfg.API.Auth.Enabled {
		t.Fatal("expected api auth enabled")
	}
	if cfg.API.Auth.BearerToken != "api-secret" {
		t.Fatalf("expected %s override, got %q", EnvAPIBearerToken, cfg.API.Auth.BearerToken)
	}
}

func TestAIConfigDefaultsDisabled(t *testing.T) {
	cfg := loadTestConfig(t, "{}\n")

	if cfg.AI.Enabled {
		t.Fatal("expected AI enrichment disabled by default")
	}
	if cfg.AI.Provider != "openai" {
		t.Fatalf("expected default AI provider openai, got %q", cfg.AI.Provider)
	}
	if cfg.AI.Model != "gpt-5.4-mini" {
		t.Fatalf("expected default AI model gpt-5.4-mini, got %q", cfg.AI.Model)
	}
	if cfg.AI.Timeout != "20s" || cfg.AI.MaxItemsPerRun != 10 || cfg.AI.QueueSize != 100 {
		t.Fatalf("unexpected AI defaults: %+v", cfg.AI)
	}
}

func TestAIConfigEnvOverrides(t *testing.T) {
	t.Setenv(EnvAIEnabled, "true")
	t.Setenv(EnvAIProvider, "openai")
	t.Setenv(EnvAIModel, "gpt-5.4-nano")
	t.Setenv(EnvAIAPIKey, "test-key")
	t.Setenv(EnvAIBaseURL, "https://example.invalid/v1")
	t.Setenv(EnvAITimeout, "5s")
	t.Setenv(EnvAIMaxItemsPerRun, "4")
	t.Setenv(EnvAIQueueSize, "12")
	t.Setenv(EnvAIIncludeIdentifiers, "true")

	cfg := loadTestConfig(t, "{}\n")

	if !cfg.AI.Enabled || cfg.AI.APIKey != "test-key" || cfg.AI.Model != "gpt-5.4-nano" {
		t.Fatalf("unexpected AI env config: %+v", cfg.AI)
	}
	if cfg.AI.BaseURL != "https://example.invalid/v1" || cfg.AI.Timeout != "5s" {
		t.Fatalf("unexpected AI endpoint config: %+v", cfg.AI)
	}
	if cfg.AI.MaxItemsPerRun != 4 || cfg.AI.QueueSize != 12 || !cfg.AI.IncludeIdentifiers {
		t.Fatalf("unexpected AI limits config: %+v", cfg.AI)
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

func TestAWSConfigDefaultsDisabled(t *testing.T) {
	cfg := loadTestConfig(t, "{}\n")

	if cfg.Providers.AWS.Enabled {
		t.Fatal("expected AWS provider disabled by default")
	}
	if cfg.Providers.AWS.IngestionMode != "cur_s3" {
		t.Fatalf("expected default AWS ingestion mode cur_s3, got %q", cfg.Providers.AWS.IngestionMode)
	}
	if cfg.Providers.AWS.Region != "us-east-1" {
		t.Fatalf("expected default AWS region us-east-1, got %q", cfg.Providers.AWS.Region)
	}
	if cfg.Providers.AWS.CostMetric != "UnblendedCost" {
		t.Fatalf("expected default AWS cost metric UnblendedCost, got %q", cfg.Providers.AWS.CostMetric)
	}
	if cfg.Providers.AWS.LookbackDays != 3 {
		t.Fatalf("expected default AWS lookback days 3, got %d", cfg.Providers.AWS.LookbackDays)
	}
	if cfg.Providers.AWS.ReportLagDays != 2 {
		t.Fatalf("expected default AWS report lag days 2, got %d", cfg.Providers.AWS.ReportLagDays)
	}
	if cfg.Providers.AWS.CURRegion != "us-east-1" {
		t.Fatalf("expected default AWS CUR region us-east-1, got %q", cfg.Providers.AWS.CURRegion)
	}
	if cfg.Providers.AWS.CURFormat != "csv_gzip" {
		t.Fatalf("expected default AWS CUR format csv_gzip, got %q", cfg.Providers.AWS.CURFormat)
	}
	if cfg.Providers.AWS.CURLookbackDays != 3 {
		t.Fatalf("expected default AWS CUR lookback days 3, got %d", cfg.Providers.AWS.CURLookbackDays)
	}
	if cfg.Providers.AWS.CURReportLagDays != 2 {
		t.Fatalf("expected default AWS CUR report lag days 2, got %d", cfg.Providers.AWS.CURReportLagDays)
	}
}

func TestAWSEnvOverrides(t *testing.T) {
	t.Setenv(EnvAWSEnabled, "true")
	t.Setenv(EnvAWSIngestionMode, "cost_explorer")
	t.Setenv("AWS_REGION", "ap-south-1")
	t.Setenv(EnvAWSCostMetric, "AmortizedCost")
	t.Setenv(EnvAWSLookbackDays, "5")
	t.Setenv(EnvAWSReportLagDays, "3")
	t.Setenv(EnvAWSCURBucket, "billing-bucket")
	t.Setenv(EnvAWSCURPrefix, "exports/cur/")
	t.Setenv(EnvAWSCURRegion, "us-east-2")
	t.Setenv(EnvAWSCURFormat, "csv")
	t.Setenv(EnvAWSCURLookbackDays, "4")
	t.Setenv(EnvAWSCURReportLagDays, "5")
	t.Setenv(EnvAWSCURLocalPath, "./testdata/aws/cur.csv.gz")

	cfg := loadTestConfig(t, "{}\n")

	if !cfg.Providers.AWS.Enabled {
		t.Fatal("expected AWS provider enabled from env")
	}
	if cfg.Providers.AWS.IngestionMode != "cost_explorer" {
		t.Fatalf("expected AWS ingestion mode env override, got %q", cfg.Providers.AWS.IngestionMode)
	}
	if cfg.Providers.AWS.Region != "ap-south-1" {
		t.Fatalf("expected AWS region env override, got %q", cfg.Providers.AWS.Region)
	}
	if cfg.Providers.AWS.CostMetric != "AmortizedCost" {
		t.Fatalf("expected AWS cost metric env override, got %q", cfg.Providers.AWS.CostMetric)
	}
	if cfg.Providers.AWS.LookbackDays != 5 {
		t.Fatalf("expected AWS lookback env override 5, got %d", cfg.Providers.AWS.LookbackDays)
	}
	if cfg.Providers.AWS.ReportLagDays != 3 {
		t.Fatalf("expected AWS report lag env override 3, got %d", cfg.Providers.AWS.ReportLagDays)
	}
	if cfg.Providers.AWS.CURBucket != "billing-bucket" || cfg.Providers.AWS.CURPrefix != "exports/cur/" {
		t.Fatalf("expected AWS CUR bucket/prefix env overrides, got %q/%q", cfg.Providers.AWS.CURBucket, cfg.Providers.AWS.CURPrefix)
	}
	if cfg.Providers.AWS.CURRegion != "us-east-2" || cfg.Providers.AWS.CURFormat != "csv" {
		t.Fatalf("expected AWS CUR region/format env overrides, got %q/%q", cfg.Providers.AWS.CURRegion, cfg.Providers.AWS.CURFormat)
	}
	if cfg.Providers.AWS.CURLookbackDays != 4 || cfg.Providers.AWS.CURReportLagDays != 5 {
		t.Fatalf("expected AWS CUR lookback/report lag env overrides, got %d/%d", cfg.Providers.AWS.CURLookbackDays, cfg.Providers.AWS.CURReportLagDays)
	}
	if cfg.Providers.AWS.CURLocalPath != "./testdata/aws/cur.csv.gz" {
		t.Fatalf("expected AWS CUR local path env override, got %q", cfg.Providers.AWS.CURLocalPath)
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

func TestAWSProviderInheritsGlobalIngestionLimits(t *testing.T) {
	provider := (AWSProvider{}).WithIngestionDefaults(Ingestion{
		MaxFilesPerRun:         7,
		MaxRecordsPerBatch:     250,
		MaxMemoryBufferRecords: 125,
		MaxRuntime:             "3m",
	})
	limits := provider.IngestionLimits()
	if limits.MaxFilesPerRun != 7 {
		t.Fatalf("MaxFilesPerRun = %d, want 7", limits.MaxFilesPerRun)
	}
	if limits.MaxRecordsPerBatch != 250 {
		t.Fatalf("MaxRecordsPerBatch = %d, want 250", limits.MaxRecordsPerBatch)
	}
	if limits.MaxMemoryBufferRecords != 125 {
		t.Fatalf("MaxMemoryBufferRecords = %d, want 125", limits.MaxMemoryBufferRecords)
	}
	if limits.MaxRuntime != 3*time.Minute {
		t.Fatalf("MaxRuntime = %s, want 3m", limits.MaxRuntime)
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
