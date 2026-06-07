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
	EnvAPIBearerToken                 = "TORVIX_API_BEARER_TOKEN"
	EnvLogLevel                       = "TORVIX_LOG_LEVEL"
	EnvLogDir                         = "TORVIX_LOG_DIR"
	EnvLogRetentionDays               = "TORVIX_LOG_RETENTION_DAYS"
	EnvReportTimezone                 = "TORVIX_REPORT_TIMEZONE"
	EnvDailyReportCron                = "TORVIX_DAILY_REPORT_CRON"
	EnvWeeklyReportCron               = "TORVIX_WEEKLY_REPORT_CRON"
	EnvReportRequireCompleteIngestion = "TORVIX_REPORT_REQUIRE_COMPLETE_INGESTION"
	EnvDailyReportTargetLagDays       = "TORVIX_DAILY_REPORT_TARGET_LAG_DAYS"
	EnvAWSEnabled                     = "TORVIX_AWS_ENABLED"
	EnvAWSIngestionMode               = "TORVIX_AWS_INGESTION_MODE"
	EnvAWSCostMetric                  = "TORVIX_AWS_COST_METRIC"
	EnvAWSLookbackDays                = "TORVIX_AWS_LOOKBACK_DAYS"
	EnvAWSReportLagDays               = "TORVIX_AWS_REPORT_LAG_DAYS"
	EnvAWSCURBucket                   = "TORVIX_AWS_CUR_BUCKET"
	EnvAWSCURPrefix                   = "TORVIX_AWS_CUR_PREFIX"
	EnvAWSCURRegion                   = "TORVIX_AWS_CUR_REGION"
	EnvAWSCURFormat                   = "TORVIX_AWS_CUR_FORMAT"
	EnvAWSCURLookbackDays             = "TORVIX_AWS_CUR_LOOKBACK_DAYS"
	EnvAWSCURReportLagDays            = "TORVIX_AWS_CUR_REPORT_LAG_DAYS"
	EnvAWSCURLocalPath                = "TORVIX_AWS_CUR_LOCAL_PATH"
	EnvWasteDetectionEnabled          = "TORVIX_WASTE_DETECTION_ENABLED"
	EnvWasteProvider                  = "TORVIX_WASTE_PROVIDER"
	EnvWasteScanIntervalHours         = "TORVIX_WASTE_SCAN_INTERVAL_HOURS"
	EnvWasteMinResourceAgeDays        = "TORVIX_WASTE_MIN_RESOURCE_AGE_DAYS"
	EnvWasteStoppedInstanceMinDays    = "TORVIX_WASTE_STOPPED_INSTANCE_MIN_DAYS"
	EnvWasteOldBackupDays             = "TORVIX_WASTE_OLD_BACKUP_DAYS"
	EnvWasteMinCostThreshold          = "TORVIX_WASTE_MIN_COST_THRESHOLD"
	EnvWasteHighMonthlyThreshold      = "TORVIX_WASTE_HIGH_MONTHLY_THRESHOLD"
	EnvWasteCurrency                  = "TORVIX_WASTE_CURRENCY"
	EnvWasteEnableTagExclusions       = "TORVIX_WASTE_ENABLE_TAG_EXCLUSIONS"
	EnvWasteExclusionTagKeys          = "TORVIX_WASTE_EXCLUSION_TAG_KEYS"
	EnvAIEnabled                      = "TORVIX_AI_ENABLED"
	EnvAIProvider                     = "TORVIX_AI_PROVIDER"
	EnvAIModel                        = "TORVIX_AI_MODEL"
	EnvAIAPIKey                       = "OPENAI_API_KEY"
	EnvAIBaseURL                      = "OPENAI_BASE_URL"
	EnvAITimeout                      = "TORVIX_AI_TIMEOUT"
	EnvAIMaxItemsPerRun               = "TORVIX_AI_MAX_ITEMS_PER_RUN"
	EnvAIQueueSize                    = "TORVIX_AI_QUEUE_SIZE"
	EnvAIIncludeIdentifiers           = "TORVIX_AI_INCLUDE_IDENTIFIERS"

	LegacyEnvReportTimezone                 = "CLOUDPULSE_REPORT_TIMEZONE"
	LegacyEnvDailyReportCron                = "CLOUDPULSE_DAILY_REPORT_CRON"
	LegacyEnvWeeklyReportCron               = "CLOUDPULSE_WEEKLY_REPORT_CRON"
	LegacyEnvReportRequireCompleteIngestion = "CLOUDPULSE_REPORT_REQUIRE_COMPLETE_INGESTION"
	LegacyEnvDailyReportTargetLagDays       = "CLOUDPULSE_DAILY_REPORT_TARGET_LAG_DAYS"
	LegacyEnvWasteDetectionEnabled          = "CLOUDPULSE_WASTE_DETECTION_ENABLED"
	LegacyEnvWasteProvider                  = "CLOUDPULSE_WASTE_PROVIDER"
	LegacyEnvWasteScanIntervalHours         = "CLOUDPULSE_WASTE_SCAN_INTERVAL_HOURS"
	LegacyEnvWasteMinResourceAgeDays        = "CLOUDPULSE_WASTE_MIN_RESOURCE_AGE_DAYS"
	LegacyEnvWasteStoppedInstanceMinDays    = "CLOUDPULSE_WASTE_STOPPED_INSTANCE_MIN_DAYS"
	LegacyEnvWasteOldBackupDays             = "CLOUDPULSE_WASTE_OLD_BACKUP_DAYS"
	LegacyEnvWasteMinCostThreshold          = "CLOUDPULSE_WASTE_MIN_COST_THRESHOLD"
	LegacyEnvWasteHighMonthlyThreshold      = "CLOUDPULSE_WASTE_HIGH_MONTHLY_THRESHOLD"
	LegacyEnvWasteCurrency                  = "CLOUDPULSE_WASTE_CURRENCY"
	LegacyEnvWasteEnableTagExclusions       = "CLOUDPULSE_WASTE_ENABLE_TAG_EXCLUSIONS"
	LegacyEnvWasteExclusionTagKeys          = "CLOUDPULSE_WASTE_EXCLUSION_TAG_KEYS"
)

type Config struct {
	LogLevel  string  `yaml:"log_level"`
	Logging   Logging `yaml:"logging"`
	HTTP      HTTP    `yaml:"http"`
	API       API     `yaml:"api"`
	DB        DB      `yaml:"db"`
	Providers struct {
		OCI Provider    `yaml:"oci"`
		AWS AWSProvider `yaml:"aws"`
	} `yaml:"providers"`
	Ingestion Ingestion `yaml:"ingestion"`
	Scheduler Scheduler `yaml:"scheduler"`
	Reporting Reporting `yaml:"reporting"`
	Metrics   Metrics   `yaml:"metrics"`
	Waste     Waste     `yaml:"waste"`
	AI        AI        `yaml:"ai"`
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

type API struct {
	Auth APIAuth `yaml:"auth"`
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

type AWSProvider struct {
	Enabled                bool   `yaml:"enabled"`
	IngestionMode          string `yaml:"ingestion_mode"`
	Region                 string `yaml:"region"`
	CostMetric             string `yaml:"cost_metric"`
	LookbackDays           int    `yaml:"lookback_days"`
	ReportLagDays          int    `yaml:"report_lag_days"`
	CURBucket              string `yaml:"cur_bucket"`
	CURPrefix              string `yaml:"cur_prefix"`
	CURRegion              string `yaml:"cur_region"`
	CURFormat              string `yaml:"cur_format"`
	CURLookbackDays        int    `yaml:"cur_lookback_days"`
	CURReportLagDays       int    `yaml:"cur_report_lag_days"`
	CURLocalPath           string `yaml:"cur_local_path"`
	MaxFilesPerRun         int    `yaml:"max_files_per_run"`
	MaxRecordsPerBatch     int    `yaml:"max_records_per_batch"`
	MaxRuntime             string `yaml:"max_runtime"`
	MaxMemoryBufferRecords int    `yaml:"max_memory_buffer_records"`
}

type Waste struct {
	DetectionEnabled       bool     `yaml:"detection_enabled"`
	Provider               string   `yaml:"provider"`
	ScanIntervalHours      int      `yaml:"scan_interval_hours"`
	MinResourceAgeDays     int      `yaml:"min_resource_age_days"`
	StoppedInstanceMinDays int      `yaml:"stopped_instance_min_days"`
	OldBackupDays          int      `yaml:"old_backup_days"`
	MinCostThreshold       float64  `yaml:"min_cost_threshold"`
	HighMonthlyThreshold   float64  `yaml:"high_monthly_threshold"`
	Currency               string   `yaml:"currency"`
	EnableTagExclusions    bool     `yaml:"enable_tag_exclusions"`
	ExclusionTagKeys       []string `yaml:"exclusion_tag_keys"`
}

type AI struct {
	Enabled            bool   `yaml:"enabled"`
	Provider           string `yaml:"provider"`
	Model              string `yaml:"model"`
	APIKey             string `yaml:"api_key"`
	BaseURL            string `yaml:"base_url"`
	Timeout            string `yaml:"timeout"`
	MaxItemsPerRun     int    `yaml:"max_items_per_run"`
	QueueSize          int    `yaml:"queue_size"`
	IncludeIdentifiers bool   `yaml:"include_identifiers"`
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

type APIAuth struct {
	Enabled     bool   `yaml:"enabled"`
	BearerToken string `yaml:"bearer_token"`
}

func Load(path string) (Config, error) {
	cfg := Config{
		Reporting: Reporting{RequireCompleteIngestion: true},
		Waste:     Waste{DetectionEnabled: true, EnableTagExclusions: true},
	}
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
	cfg.Providers.AWS = cfg.Providers.AWS.WithDefaults()
	cfg.Waste = cfg.Waste.WithDefaults()
	cfg.AI = cfg.AI.WithDefaults()
	return cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	if address := envValue(EnvHTTPAddress); address != "" {
		cfg.HTTP.Address = address
	} else if port := envValue(EnvHTTPPort, EnvAPIPort); port != "" {
		cfg.HTTP.Address = normalizeHTTPPort(port)
	}
	if token := envValue(EnvAPIBearerToken); token != "" {
		cfg.API.Auth.Enabled = true
		cfg.API.Auth.BearerToken = token
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
	if enabled := envValue(EnvAWSEnabled); enabled != "" {
		if parsed, err := strconv.ParseBool(enabled); err == nil {
			cfg.Providers.AWS.Enabled = parsed
		}
	}
	if mode := envValue(EnvAWSIngestionMode); mode != "" {
		cfg.Providers.AWS.IngestionMode = mode
	}
	if region := envValue("AWS_REGION"); region != "" {
		cfg.Providers.AWS.Region = region
	}
	if metric := envValue(EnvAWSCostMetric); metric != "" {
		cfg.Providers.AWS.CostMetric = metric
	}
	if lookback := envValue(EnvAWSLookbackDays); lookback != "" {
		if days, err := strconv.Atoi(lookback); err == nil {
			cfg.Providers.AWS.LookbackDays = days
		}
	}
	if lag := envValue(EnvAWSReportLagDays); lag != "" {
		if days, err := strconv.Atoi(lag); err == nil {
			cfg.Providers.AWS.ReportLagDays = days
		}
	}
	if bucket := envValue(EnvAWSCURBucket); bucket != "" {
		cfg.Providers.AWS.CURBucket = bucket
	}
	if prefix := envValue(EnvAWSCURPrefix); prefix != "" {
		cfg.Providers.AWS.CURPrefix = prefix
	}
	if region := envValue(EnvAWSCURRegion); region != "" {
		cfg.Providers.AWS.CURRegion = region
	}
	if format := envValue(EnvAWSCURFormat); format != "" {
		cfg.Providers.AWS.CURFormat = format
	}
	if lookback := envValue(EnvAWSCURLookbackDays); lookback != "" {
		if days, err := strconv.Atoi(lookback); err == nil {
			cfg.Providers.AWS.CURLookbackDays = days
		}
	}
	if lag := envValue(EnvAWSCURReportLagDays); lag != "" {
		if days, err := strconv.Atoi(lag); err == nil {
			cfg.Providers.AWS.CURReportLagDays = days
		}
	}
	if localPath := envValue(EnvAWSCURLocalPath); localPath != "" {
		cfg.Providers.AWS.CURLocalPath = localPath
	}
	if enabled := envValue(EnvWasteDetectionEnabled, LegacyEnvWasteDetectionEnabled); enabled != "" {
		if parsed, err := strconv.ParseBool(enabled); err == nil {
			cfg.Waste.DetectionEnabled = parsed
		}
	}
	if provider := envValue(EnvWasteProvider, LegacyEnvWasteProvider); provider != "" {
		cfg.Waste.Provider = provider
	}
	if hours := envValue(EnvWasteScanIntervalHours, LegacyEnvWasteScanIntervalHours); hours != "" {
		if parsed, err := strconv.Atoi(hours); err == nil {
			cfg.Waste.ScanIntervalHours = parsed
		}
	}
	if days := envValue(EnvWasteMinResourceAgeDays, LegacyEnvWasteMinResourceAgeDays); days != "" {
		if parsed, err := strconv.Atoi(days); err == nil {
			cfg.Waste.MinResourceAgeDays = parsed
		}
	}
	if days := envValue(EnvWasteStoppedInstanceMinDays, LegacyEnvWasteStoppedInstanceMinDays); days != "" {
		if parsed, err := strconv.Atoi(days); err == nil {
			cfg.Waste.StoppedInstanceMinDays = parsed
		}
	}
	if days := envValue(EnvWasteOldBackupDays, LegacyEnvWasteOldBackupDays); days != "" {
		if parsed, err := strconv.Atoi(days); err == nil {
			cfg.Waste.OldBackupDays = parsed
		}
	}
	if threshold := envValue(EnvWasteMinCostThreshold, LegacyEnvWasteMinCostThreshold); threshold != "" {
		if parsed, err := strconv.ParseFloat(threshold, 64); err == nil {
			cfg.Waste.MinCostThreshold = parsed
		}
	}
	if threshold := envValue(EnvWasteHighMonthlyThreshold, LegacyEnvWasteHighMonthlyThreshold); threshold != "" {
		if parsed, err := strconv.ParseFloat(threshold, 64); err == nil {
			cfg.Waste.HighMonthlyThreshold = parsed
		}
	}
	if currency := envValue(EnvWasteCurrency, LegacyEnvWasteCurrency); currency != "" {
		cfg.Waste.Currency = currency
	}
	if enabled := envValue(EnvWasteEnableTagExclusions, LegacyEnvWasteEnableTagExclusions); enabled != "" {
		if parsed, err := strconv.ParseBool(enabled); err == nil {
			cfg.Waste.EnableTagExclusions = parsed
		}
	}
	if keys := envValue(EnvWasteExclusionTagKeys, LegacyEnvWasteExclusionTagKeys); keys != "" {
		cfg.Waste.ExclusionTagKeys = splitCSV(keys)
	}
	if enabled := envValue(EnvAIEnabled); enabled != "" {
		if parsed, err := strconv.ParseBool(enabled); err == nil {
			cfg.AI.Enabled = parsed
		}
	}
	if provider := envValue(EnvAIProvider); provider != "" {
		cfg.AI.Provider = provider
	}
	if model := envValue(EnvAIModel); model != "" {
		cfg.AI.Model = model
	}
	if apiKey := envValue(EnvAIAPIKey); apiKey != "" {
		cfg.AI.APIKey = apiKey
	}
	if baseURL := envValue(EnvAIBaseURL); baseURL != "" {
		cfg.AI.BaseURL = baseURL
	}
	if timeout := envValue(EnvAITimeout); timeout != "" {
		cfg.AI.Timeout = timeout
	}
	if limit := envValue(EnvAIMaxItemsPerRun); limit != "" {
		if parsed, err := strconv.Atoi(limit); err == nil {
			cfg.AI.MaxItemsPerRun = parsed
		}
	}
	if size := envValue(EnvAIQueueSize); size != "" {
		if parsed, err := strconv.Atoi(size); err == nil {
			cfg.AI.QueueSize = parsed
		}
	}
	if include := envValue(EnvAIIncludeIdentifiers); include != "" {
		if parsed, err := strconv.ParseBool(include); err == nil {
			cfg.AI.IncludeIdentifiers = parsed
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

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
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

func (a AWSProvider) WithDefaults() AWSProvider {
	a.IngestionMode = strings.TrimSpace(strings.ToLower(a.IngestionMode))
	if a.IngestionMode == "" {
		a.IngestionMode = "cur_s3"
	}
	if a.Region == "" {
		a.Region = "us-east-1"
	}
	if a.CostMetric == "" {
		a.CostMetric = "UnblendedCost"
	}
	if a.LookbackDays <= 0 {
		a.LookbackDays = 3
	}
	if a.ReportLagDays <= 0 {
		a.ReportLagDays = 2
	}
	if a.CURRegion == "" {
		a.CURRegion = a.Region
	}
	a.CURFormat = strings.TrimSpace(strings.ToLower(a.CURFormat))
	if a.CURFormat == "" {
		a.CURFormat = "csv_gzip"
	}
	if a.CURLookbackDays <= 0 {
		a.CURLookbackDays = 3
	}
	if a.CURReportLagDays <= 0 {
		a.CURReportLagDays = a.ReportLagDays
	}
	if a.CURReportLagDays <= 0 {
		a.CURReportLagDays = 2
	}
	return a
}

func (a AWSProvider) WithIngestionDefaults(ingestion Ingestion) AWSProvider {
	ingestion = ingestion.WithDefaults()
	if a.MaxFilesPerRun <= 0 {
		a.MaxFilesPerRun = ingestion.MaxFilesPerRun
	}
	if a.MaxRecordsPerBatch <= 0 {
		a.MaxRecordsPerBatch = ingestion.MaxRecordsPerBatch
	}
	if a.MaxMemoryBufferRecords <= 0 {
		a.MaxMemoryBufferRecords = ingestion.MaxMemoryBufferRecords
	}
	if a.MaxRuntime == "" {
		a.MaxRuntime = ingestion.MaxRuntime
	}
	return a
}

func (a AWSProvider) IngestionLimits() IngestionLimits {
	provider := Provider{
		MaxFilesPerRun:         a.MaxFilesPerRun,
		MaxRecordsPerBatch:     a.MaxRecordsPerBatch,
		MaxRuntime:             a.MaxRuntime,
		MaxMemoryBufferRecords: a.MaxMemoryBufferRecords,
	}
	return provider.IngestionLimits()
}

func (w Waste) WithDefaults() Waste {
	w.Provider = strings.TrimSpace(strings.ToLower(w.Provider))
	if w.Provider == "" {
		w.Provider = "oci"
	}
	if w.ScanIntervalHours <= 0 {
		w.ScanIntervalHours = 24
	}
	if w.MinResourceAgeDays <= 0 {
		w.MinResourceAgeDays = 7
	}
	if w.StoppedInstanceMinDays <= 0 {
		w.StoppedInstanceMinDays = 3
	}
	if w.OldBackupDays <= 0 {
		w.OldBackupDays = 30
	}
	if w.HighMonthlyThreshold <= 0 {
		w.HighMonthlyThreshold = 50
	}
	if w.Currency == "" {
		w.Currency = "USD"
	}
	if len(w.ExclusionTagKeys) == 0 {
		w.ExclusionTagKeys = []string{"torvix:ignore", "torvix:waste-ignore", "finops:ignore", "keep", "retain", "do-not-delete"}
	}
	return w
}

func (a AI) WithDefaults() AI {
	a.Provider = strings.TrimSpace(strings.ToLower(a.Provider))
	if a.Provider == "" {
		a.Provider = "openai"
	}
	if a.Model == "" {
		a.Model = "gpt-5.4-mini"
	}
	if a.BaseURL == "" {
		a.BaseURL = "https://api.openai.com/v1"
	}
	if a.Timeout == "" {
		a.Timeout = "20s"
	}
	if a.MaxItemsPerRun <= 0 {
		a.MaxItemsPerRun = 10
	}
	if a.QueueSize <= 0 {
		a.QueueSize = 100
	}
	return a
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
