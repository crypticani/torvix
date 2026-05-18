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
