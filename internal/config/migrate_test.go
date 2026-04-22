package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateV2ToV3_LegacyFields(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`
upstream = "https://legacy.example.com"
upstream_protocol = "openai"
listen = ":443"
hijack = "openrouter.ai"
[models]
"openai/gpt-5" = "gpt-4o"
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := saveSchemaVersion(cfgPath, 2); err != nil {
		t.Fatalf("saveSchemaVersion: %v", err)
	}

	cfg, err := Load(cfgPath, nil)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	changed, report, err := Migrate(cfgPath, cfg)
	if err != nil {
		t.Fatalf("Migrate() error: %v", err)
	}
	if !changed {
		t.Fatal("expected migration to change config")
	}
	if len(report) == 0 {
		t.Fatal("expected migration report")
	}
	if cfg.Upstream != "" || cfg.UpstreamProtocol != "" {
		t.Fatalf("legacy fields not cleared: upstream=%q protocol=%q", cfg.Upstream, cfg.UpstreamProtocol)
	}
	if got := loadSchemaVersion(cfgPath); got != 3 {
		t.Fatalf("schema version = %d, want 3", got)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "upstream =") || strings.Contains(text, "upstream_protocol =") {
		t.Fatalf("migrated config still contains legacy fields:\n%s", text)
	}
	if !strings.Contains(text, "[upstreams.default]") {
		t.Fatalf("migrated config missing [upstreams.default]:\n%s", text)
	}
}

func TestMigrateV2ToV3_AlreadyV3(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`
[upstreams.default]
url = "https://api.example.com"
protocol = "anthropic"
default = true
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := saveSchemaVersion(cfgPath, 3); err != nil {
		t.Fatalf("saveSchemaVersion: %v", err)
	}

	cfg, err := Load(cfgPath, nil)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	changed, report, err := Migrate(cfgPath, cfg)
	if err != nil {
		t.Fatalf("Migrate() error: %v", err)
	}
	if changed {
		t.Fatal("expected already-v3 config to skip migration")
	}
	if report != nil {
		t.Fatalf("expected nil report, got %#v", report)
	}
}

func TestMigrateV2ToV3_MixedFormatPreservesNamedUpstreams(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`
upstream = "https://legacy.example.com"
upstream_protocol = "openai"

[upstreams.default]
url = "https://anthropic.example.com"
protocol = "anthropic"
default = true

[upstreams.alt]
url = "https://openai.example.com/v1"
protocol = "openai"

[models]
"openai/gpt-5" = { upstream = "alt", model = "gpt-4o" }
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := saveSchemaVersion(cfgPath, 2); err != nil {
		t.Fatalf("saveSchemaVersion: %v", err)
	}

	cfg, err := Load(cfgPath, nil)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	changed, report, err := Migrate(cfgPath, cfg)
	if err != nil {
		t.Fatalf("Migrate() error: %v", err)
	}
	if !changed {
		t.Fatal("expected migration to change config")
	}
	if len(report) == 0 {
		t.Fatal("expected migration report")
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "\nupstream =") || strings.Contains(text, "\nupstream_protocol =") {
		t.Fatalf("migrated config still contains legacy fields:\n%s", text)
	}
	if !strings.Contains(text, "[upstreams.alt]") {
		t.Fatalf("migrated config lost named upstream:\n%s", text)
	}
}

func TestMigrate_MissingConfigFile(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.toml")

	changed, report, err := Migrate(cfgPath, DefaultConfig())
	if err != nil {
		t.Fatalf("Migrate() error: %v", err)
	}
	if changed {
		t.Fatal("expected missing config to skip migration")
	}
	if report != nil {
		t.Fatalf("expected nil report, got %#v", report)
	}
	if _, statErr := os.Stat(cfgPath); !os.IsNotExist(statErr) {
		t.Fatalf("config file should not be created, stat err = %v", statErr)
	}
}
