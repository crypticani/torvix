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
		AWS   Provider `yaml:"aws"`
		Azure Provider `yaml:"azure"`
		GCP   Provider `yaml:"gcp"`
		OCI   Provider `yaml:"oci"`
	} `yaml:"providers"`
	Reporting Reporting `yaml:"reporting"`
	Metrics   Metrics   `yaml:"metrics"`
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
	Enabled       bool   `yaml:"enabled"`
	Bucket        string `yaml:"bucket"`
	Prefix        string `yaml:"prefix"`
	Account       string `yaml:"account"`
	Container     string `yaml:"container"`
	Project       string `yaml:"project"`
	Namespace     string `yaml:"namespace"`
	Region        string `yaml:"region"`
	Credentials   string `yaml:"credentials"`
	ConfigFile    string `yaml:"config_file"`
	ConfigProfile string `yaml:"config_profile"`
	Passphrase    string `yaml:"passphrase"`
	PollSchedule  string `yaml:"poll_schedule"`
	Format        string `yaml:"format"`
	LookbackDays  int    `yaml:"lookback_days"`
	MaxObjectScan int    `yaml:"max_object_scan"`
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
	return cfg, nil
}

func (p Provider) Lookback() time.Duration {
	if p.LookbackDays <= 0 {
		return 24 * time.Hour
	}
	return time.Duration(p.LookbackDays) * 24 * time.Hour
}
