package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	LogLevel  string `yaml:"log_level"`
	HTTP      HTTP   `yaml:"http"`
	DB        DB     `yaml:"db"`
	Providers struct {
		OCI Provider `yaml:"oci"`
	} `yaml:"providers"`
	Ingestion Ingestion `yaml:"ingestion"`
	Scheduler Scheduler `yaml:"scheduler"`
	Reporting Reporting `yaml:"reporting"`
	Metrics   Metrics   `yaml:"metrics"`
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
	MaxRecordsPerBatch     int    `yaml:"max_records_per_batch"`
	MaxRuntime             string `yaml:"max_runtime"`
	MaxMemoryBufferRecords int    `yaml:"max_memory_buffer_records"`
	DryRun                 bool   `yaml:"dry_run"`
	SampleMode             bool   `yaml:"sample_mode"`
}

type IngestionLimits struct {
	MaxFilesPerRun         int
	MaxRecordsPerBatch     int
	MaxRuntime             time.Duration
	MaxMemoryBufferRecords int
	DryRun                 bool
	SampleMode             bool
}

type Reporting struct {
	Webhooks []Webhook `yaml:"webhooks"`
}

type Webhook struct {
	Name    string `yaml:"name"`
	Type    string `yaml:"type"`
	URL     string `yaml:"url"`
	Enabled bool   `yaml:"enabled"`
}

type Metrics struct {
	Namespace string `yaml:"namespace"`
}

func Load(path string) (Config, error) {
	var cfg Config
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	if cfg.HTTP.Address == "" {
		cfg.HTTP.Address = ":8080"
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if cfg.Metrics.Namespace == "" {
		cfg.Metrics.Namespace = "cloudpulse"
	}
	cfg.Ingestion = cfg.Ingestion.WithDefaults()
	return cfg, nil
}

func (i Ingestion) WithDefaults() Ingestion {
	if i.LookbackDays <= 0 {
		i.LookbackDays = 30
	}
	if i.RetentionDays <= 0 {
		i.RetentionDays = 180
	}
	if i.CompressionAfterDays <= 0 {
		i.CompressionAfterDays = 7
	}
	if i.MaxFilesPerRun <= 0 {
		i.MaxFilesPerRun = 25
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
		MaxRecordsPerBatch:     p.MaxRecordsPerBatch,
		MaxMemoryBufferRecords: p.MaxMemoryBufferRecords,
		DryRun:                 p.DryRun,
		SampleMode:             p.SampleMode,
	}
	if limits.MaxFilesPerRun <= 0 {
		limits.MaxFilesPerRun = 25
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
