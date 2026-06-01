package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	EnvHTTPAddress                    = "TORVIX_HTTP_ADDRESS"
	EnvHTTPPort                       = "TORVIX_HTTP_PORT"
	EnvAPIPort                        = "TORVIX_API_PORT"
	EnvGrafanaAPIBearerToken          = "TORVIX_GRAFANA_API_BEARER_TOKEN"
	EnvLogLevel                       = "TORVIX_LOG_LEVEL"
	EnvLogDir                         = "TORVIX_LOG_DIR"
	EnvLogRetentionDays               = "TORVIX_LOG_RETENTION_DAYS"
	EnvReportTimezone                 = "TORVIX_REPORT_TIMEZONE"
	EnvDailyReportCron                = "TORVIX_DAILY_REPORT_CRON"
	EnvWeeklyReportCron               = "TORVIX_WEEKLY_REPORT_CRON"
	EnvReportRequireCompleteIngestion = "TORVIX_REPORT_REQUIRE_COMPLETE_INGESTION"
	EnvDailyReportTargetLagDays       = "TORVIX_DAILY_REPORT_TARGET_LAG_DAYS"

	LegacyEnvReportTimezone                 = "CLOUDPULSE_REPORT_TIMEZONE"
	LegacyEnvDailyReportCron                = "CLOUDPULSE_DAILY_REPORT_CRON"
	LegacyEnvWeeklyReportCron               = "CLOUDPULSE_WEEKLY_REPORT_CRON"
	LegacyEnvReportRequireCompleteIngestion = "CLOUDPULSE_REPORT_REQUIRE_COMPLETE_INGESTION"
	LegacyEnvDailyReportTargetLagDays       = "CLOUDPULSE_DAILY_REPORT_TARGET_LAG_DAYS"
)

type Config struct {
	LogLevel  string  `yaml:"log_level"`
	Logging   Logging `yaml:"logging"`
	HTTP      HTTP    `yaml:"http"`
	DB        DB      `yaml:"db"`
	Providers struct {
		OCI Provider `yaml:"oci"`
	} `yaml:"providers"`
	Ingestion Ingestion `yaml:"ingestion"`
	Scheduler Scheduler `yaml:"scheduler"`
	Reporting Reporting `yaml:"reporting"`
	Metrics   Metrics   `yaml:"metrics"`
	Grafana   Grafana   `yaml:"grafana"`
}

type Logging struct {
	Level         string `yaml:"level"`
	Dir           string `yaml:"dir"`
	RetentionDays int    `yaml:"retention_days"`
}

type Scheduler struct {
	Enabled        bool   `yaml:"enabled"`
	IngestInterval string `yaml:"ingest_interval"`
}

type HTTP struct {
	Address string `yaml:"address"`
}

type DB struct {
	DSN      string `yaml:"dsn"`
	MaxConns int32  `yaml:"max_conns"`
	MinConns int32  `yaml:"min_conns"`
}

type Provider struct {
	Enabled                bool   `yaml:"enabled"`
	Bucket                 string `yaml:"bucket"`
	Prefix                 string `yaml:"prefix"`
	Account                string `yaml:"account"`
	Namespace              string `yaml:"namespace"`
	ConfigFile             string `yaml:"config_file"`
	ConfigProfile          string `yaml:"config_profile"`
	Passphrase             string `yaml:"passphrase"`
	LookbackDays           int    `yaml:"lookback_days"`
	MaxObjectScan          int    `yaml:"max_object_scan"`
	MaxZeroYieldFiles      int    `yaml:"max_zero_yield_files"`
	MaxFilesPerRun         int    `yaml:"max_files_per_run"`
	MaxRecordsPerBatch     int    `yaml:"max_records_per_batch"`
	MaxRuntime             string `yaml:"max_runtime"`
	MaxMemoryBufferRecords int    `yaml:"max_memory_buffer_records"`
	DryRun                 bool   `yaml:"dry_run"`
	SampleMode             bool   `yaml:"sample_mode"`
}

type Ingestion struct {
	LookbackDays           int    `yaml:"lookback_days"`
	RetentionDays          int    `yaml:"retention_days"`
	CompressionAfterDays   int    `yaml:"compression_after_days"`
	MaxFilesPerRun         int    `yaml:"max_files_per_run"`
	MaxZeroYieldFiles      int    `yaml:"max_zero_yield_files"`
	MaxRecordsPerBatch     int    `yaml:"max_records_per_batch"`
	MaxRuntime             string `yaml:"max_runtime"`
	MaxMemoryBufferRecords int    `yaml:"max_memory_buffer_records"`
	DryRun                 bool   `yaml:"dry_run"`
	SampleMode             bool   `yaml:"sample_mode"`
}

type IngestionLimits struct {
	MaxFilesPerRun         int
	MaxZeroYieldFiles      int
	MaxRecordsPerBatch     int
	MaxRuntime             time.Duration
	MaxMemoryBufferRecords int
	DryRun                 bool
	SampleMode             bool
}

type Reporting struct {
	Timezone                 string    `yaml:"timezone"`
	DailyReportCron          string    `yaml:"daily_report_cron"`
	WeeklyReportCron         string    `yaml:"weekly_report_cron"`
	RequireCompleteIngestion bool      `yaml:"require_complete_ingestion"`
	DailyReportTargetLagDays int       `yaml:"daily_report_target_lag_days"`
	Webhooks                 []Webhook `yaml:"webhooks"`
}

type Webhook struct {
	Name          string   `yaml:"name"`
	Type          string   `yaml:"type"`
	URL           string   `yaml:"url"`
	Enabled       bool     `yaml:"enabled"`
	Currency      string   `yaml:"currency"`
	BotToken      string   `yaml:"bot_token"`
	ChatID        string   `yaml:"chat_id"`
	ParseMode     string   `yaml:"parse_mode"`
	SMTPHost      string   `yaml:"smtp_host"`
	SMTPPort      int      `yaml:"smtp_port"`
	Username      string   `yaml:"username"`
	Password      string   `yaml:"password"`
	From          string   `yaml:"from"`
	To            []string `yaml:"to"`
	SubjectPrefix string   `yaml:"subject_prefix"`
}

type Metrics struct {
	Namespace        string `yaml:"namespace"`
	CostStatsEnabled bool   `yaml:"cost_stats_enabled"`
}

type Grafana struct {
	APIAuth GrafanaAPIAuth `yaml:"api_auth"`
}

type GrafanaAPIAuth struct {
	Enabled     bool   `yaml:"enabled"`
	BearerToken string `yaml:"bearer_token"`
}

func Load(path string) (Config, error) {
	cfg := Config{Reporting: Reporting{RequireCompleteIngestion: true}}
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	applyEnvOverrides(&cfg)
	if cfg.HTTP.Address == "" {
		cfg.HTTP.Address = ":8080"
	}
	cfg.Logging = cfg.Logging.WithDefaults(cfg.LogLevel)
	cfg.LogLevel = cfg.Logging.Level
	if cfg.Metrics.Namespace == "" {
		cfg.Metrics.Namespace = "torvix"
	}
	cfg.Ingestion = cfg.Ingestion.WithDefaults()
	cfg.Scheduler = cfg.Scheduler.WithDefaults()
	cfg.Reporting = cfg.Reporting.WithDefaults()
	return cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	if address := envValue(EnvHTTPAddress); address != "" {
		cfg.HTTP.Address = address
	} else if port := envValue(EnvHTTPPort, EnvAPIPort); port != "" {
		cfg.HTTP.Address = normalizeHTTPPort(port)
	}
	if token := envValue(EnvGrafanaAPIBearerToken); token != "" {
		cfg.Grafana.APIAuth.Enabled = true
		cfg.Grafana.APIAuth.BearerToken = token
	}
	if level := envValue(EnvLogLevel); level != "" {
		cfg.Logging.Level = level
	}
	if dir := envValue(EnvLogDir); dir != "" {
		cfg.Logging.Dir = dir
	}
	if retention := envValue(EnvLogRetentionDays); retention != "" {
		if days, err := strconv.Atoi(retention); err == nil {
			cfg.Logging.RetentionDays = days
		}
	}
	if timezone := envValue(EnvReportTimezone, LegacyEnvReportTimezone); timezone != "" {
		cfg.Reporting.Timezone = timezone
	}
	if cron := envValue(EnvDailyReportCron, LegacyEnvDailyReportCron); cron != "" {
		cfg.Reporting.DailyReportCron = cron
	}
	if cron := envValue(EnvWeeklyReportCron, LegacyEnvWeeklyReportCron); cron != "" {
		cfg.Reporting.WeeklyReportCron = cron
	}
	if require := envValue(EnvReportRequireCompleteIngestion, LegacyEnvReportRequireCompleteIngestion); require != "" {
		if parsed, err := strconv.ParseBool(require); err == nil {
			cfg.Reporting.RequireCompleteIngestion = parsed
		}
	}
	if lag := envValue(EnvDailyReportTargetLagDays, LegacyEnvDailyReportTargetLagDays); lag != "" {
		if days, err := strconv.Atoi(lag); err == nil {
			cfg.Reporting.DailyReportTargetLagDays = days
		}
	}
}

func envValue(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func normalizeHTTPPort(port string) string {
	if strings.Contains(port, ":") {
		return port
	}
	return ":" + port
}

func (s Scheduler) WithDefaults() Scheduler {
	if s.IngestInterval == "" {
		s.IngestInterval = "24h"
	}
	return s
}

func (r Reporting) WithDefaults() Reporting {
	if r.Timezone == "" {
		r.Timezone = "Asia/Kolkata"
	}
	if r.DailyReportCron == "" {
		r.DailyReportCron = "0 14 * * *"
	}
	if r.WeeklyReportCron == "" {
		r.WeeklyReportCron = "0 15 * * 1"
	}
	if r.DailyReportTargetLagDays <= 0 {
		r.DailyReportTargetLagDays = 1
	}
	return r
}

func (l Logging) WithDefaults(legacyLevel string) Logging {
	if l.Level == "" {
		l.Level = legacyLevel
	}
	if l.Level == "" {
		l.Level = "info"
	}
	if l.Dir == "" {
		l.Dir = "logs"
	}
	if l.RetentionDays <= 0 {
		l.RetentionDays = 14
	}
	return l
}

func (i Ingestion) WithDefaults() Ingestion {
	if i.LookbackDays <= 0 {
		i.LookbackDays = 30
	}
	if i.RetentionDays <= 0 {
		i.RetentionDays = 90
	}
	if i.CompressionAfterDays <= 0 {
		i.CompressionAfterDays = 7
	}
	if i.MaxFilesPerRun <= 0 {
		i.MaxFilesPerRun = 25
	}
	if i.MaxZeroYieldFiles <= 0 {
		i.MaxZeroYieldFiles = 25
	}
	if i.MaxRecordsPerBatch <= 0 {
		i.MaxRecordsPerBatch = 1000
	}
	if i.MaxMemoryBufferRecords <= 0 {
		i.MaxMemoryBufferRecords = i.MaxRecordsPerBatch
	}
	if i.MaxRuntime == "" {
		i.MaxRuntime = "10m"
	}
	return i
}

func (p Provider) WithIngestionDefaults(ingestion Ingestion) Provider {
	ingestion = ingestion.WithDefaults()
	if p.LookbackDays <= 0 {
		p.LookbackDays = ingestion.LookbackDays
	}
	if p.MaxFilesPerRun <= 0 {
		p.MaxFilesPerRun = ingestion.MaxFilesPerRun
	}
	if p.MaxZeroYieldFiles <= 0 {
		p.MaxZeroYieldFiles = ingestion.MaxZeroYieldFiles
	}
	if p.MaxRecordsPerBatch <= 0 {
		p.MaxRecordsPerBatch = ingestion.MaxRecordsPerBatch
	}
	if p.MaxMemoryBufferRecords <= 0 {
		p.MaxMemoryBufferRecords = ingestion.MaxMemoryBufferRecords
	}
	if p.MaxRuntime == "" {
		p.MaxRuntime = ingestion.MaxRuntime
	}
	p.DryRun = p.DryRun || ingestion.DryRun
	p.SampleMode = p.SampleMode || ingestion.SampleMode
	return p
}

func (p Provider) IngestionLimits() IngestionLimits {
	limits := IngestionLimits{
		MaxFilesPerRun:         p.MaxFilesPerRun,
		MaxZeroYieldFiles:      p.MaxZeroYieldFiles,
		MaxRecordsPerBatch:     p.MaxRecordsPerBatch,
		MaxMemoryBufferRecords: p.MaxMemoryBufferRecords,
		DryRun:                 p.DryRun,
		SampleMode:             p.SampleMode,
	}
	if limits.MaxFilesPerRun <= 0 {
		limits.MaxFilesPerRun = 25
	}
	if limits.MaxZeroYieldFiles <= 0 {
		limits.MaxZeroYieldFiles = 25
	}
	if limits.MaxRecordsPerBatch <= 0 {
		limits.MaxRecordsPerBatch = 1000
	}
	if limits.MaxMemoryBufferRecords <= 0 {
		limits.MaxMemoryBufferRecords = limits.MaxRecordsPerBatch
	}
	if limits.MaxMemoryBufferRecords > limits.MaxRecordsPerBatch {
		limits.MaxMemoryBufferRecords = limits.MaxRecordsPerBatch
	}
	if p.MaxRuntime == "" {
		limits.MaxRuntime = 10 * time.Minute
	} else if d, err := time.ParseDuration(p.MaxRuntime); err == nil && d > 0 {
		limits.MaxRuntime = d
	} else {
		limits.MaxRuntime = 10 * time.Minute
	}
	if limits.SampleMode {
		if limits.MaxFilesPerRun > 3 {
			limits.MaxFilesPerRun = 3
		}
		if limits.MaxZeroYieldFiles > 3 {
			limits.MaxZeroYieldFiles = 3
		}
		if limits.MaxRecordsPerBatch > 100 {
			limits.MaxRecordsPerBatch = 100
		}
		if limits.MaxMemoryBufferRecords > limits.MaxRecordsPerBatch {
			limits.MaxMemoryBufferRecords = limits.MaxRecordsPerBatch
		}
		if limits.MaxRuntime > 2*time.Minute {
			limits.MaxRuntime = 2 * time.Minute
		}
	}
	return limits
}
