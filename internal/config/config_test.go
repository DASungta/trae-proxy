package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLogLevelDefault(t *testing.T) {
	cfg, err := Load("", nil)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("default LogLevel = %q, want info", cfg.LogLevel)
	}
	if cfg.LogBody {
		t.Errorf("default LogBody = true, want false")
	}
}

func TestLoadLogLevelFromTOML(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`
upstream = "http://localhost:8080"
log_level = "debug"
log_body = true
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(cfgPath, nil)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug", cfg.LogLevel)
	}
	if !cfg.LogBody {
		t.Errorf("LogBody = false, want true")
	}
}

func TestLoadLogLevelFromOverride(t *testing.T) {
	cfg, err := Load("", map[string]string{"log_level": "trace"})
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.LogLevel != "trace" {
		t.Errorf("LogLevel = %q, want trace", cfg.LogLevel)
	}
}

func TestLoadLogBodyFromOverride(t *testing.T) {
	cfg, err := Load("", map[string]string{"log_body": "true"})
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !cfg.LogBody {
		t.Errorf("LogBody = false, want true")
	}
}

func TestLoadLogLevelFromEnv(t *testing.T) {
	os.Setenv("TRAE_LOG_LEVEL", "warn")
	defer os.Unsetenv("TRAE_LOG_LEVEL")

	cfg, err := Load("", nil)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("LogLevel = %q, want warn", cfg.LogLevel)
	}
}

func TestLoadLogLevelOverrideTakesPriorityOverEnv(t *testing.T) {
	os.Setenv("TRAE_LOG_LEVEL", "warn")
	defer os.Unsetenv("TRAE_LOG_LEVEL")

	// CLI override should win over env var.
	cfg, err := Load("", map[string]string{"log_level": "error"})
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.LogLevel != "error" {
		t.Errorf("LogLevel = %q, want error (override should beat env)", cfg.LogLevel)
	}
}

func TestLoadLogLevelInvalid(t *testing.T) {
	_, err := Load("", map[string]string{"log_level": "verbose"})
	if err == nil {
		t.Errorf("expected error for invalid log_level, got nil")
	}
}
