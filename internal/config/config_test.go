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

func TestLoad_LegacyUpstreamField(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`
upstream = "https://legacy.example.com/v1/messages"
upstream_protocol = "anthropic"
[models]
"anthropic/claude-sonnet-4.6" = "claude-sonnet-4.6"
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(cfgPath, nil)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	upstream := cfg.DefaultUpstream()
	if upstream == nil {
		t.Fatal("DefaultUpstream() = nil")
	}
	if upstream.URL != "https://legacy.example.com/v1/messages" {
		t.Fatalf("upstream.URL = %q", upstream.URL)
	}
	if got := upstream.ResolveURL("/v1/messages"); got != "https://legacy.example.com/v1/messages" {
		t.Fatalf("ResolveURL(/v1/messages) = %q", got)
	}
	route, err := cfg.RouteModel("anthropic/claude-sonnet-4.6")
	if err != nil {
		t.Fatalf("RouteModel() error: %v", err)
	}
	if route.Upstream != upstream {
		t.Fatal("route.Upstream did not use in-memory default upstream")
	}
	if route.UpstreamModel != "claude-sonnet-4.6" {
		t.Fatalf("route.UpstreamModel = %q", route.UpstreamModel)
	}
}

func TestLoad_NamedUpstreams_StringModel(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`
[upstreams.default]
url = "https://anthropic.example.com"
protocol = "anthropic"
default = true

[models]
"anthropic/claude-sonnet-4.6" = "claude-sonnet-4.6"
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(cfgPath, nil)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	route, err := cfg.RouteModel("anthropic/claude-sonnet-4.6")
	if err != nil {
		t.Fatalf("RouteModel() error: %v", err)
	}
	if route.Upstream != cfg.DefaultUpstream() {
		t.Fatal("string model route should use default upstream")
	}
}

func TestLoad_NamedUpstreams_TableModel(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`
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

	cfg, err := Load(cfgPath, nil)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	route, err := cfg.RouteModel("openai/gpt-5")
	if err != nil {
		t.Fatalf("RouteModel() error: %v", err)
	}
	if route.Upstream != cfg.Upstreams["alt"] {
		t.Fatal("table model route should use named upstream")
	}
	if route.UpstreamModel != "gpt-4o" {
		t.Fatalf("route.UpstreamModel = %q, want gpt-4o", route.UpstreamModel)
	}
}

func TestLoad_MultipleDefaults(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`
[upstreams.a]
url = "https://a.example.com"
protocol = "anthropic"
default = true

[upstreams.b]
url = "https://b.example.com"
protocol = "openai"
default = true
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := Load(cfgPath, nil); err == nil {
		t.Fatal("expected error for multiple defaults")
	}
}

func TestLoad_NoDefault(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`
[upstreams.a]
url = "https://a.example.com"
protocol = "anthropic"
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := Load(cfgPath, nil); err == nil {
		t.Fatal("expected error for missing default upstream")
	}
}

func TestLoad_UnknownUpstreamRef(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`
[upstreams.default]
url = "https://anthropic.example.com"
protocol = "anthropic"
default = true

[models]
"openai/gpt-5" = { upstream = "missing", model = "gpt-4o" }
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := Load(cfgPath, nil); err == nil {
		t.Fatal("expected error for unknown upstream reference")
	}
}

func TestRouteModel_ThreeTierFallback(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RawModels = map[string]any{
		"claude-sonnet-4.6": "sonnet-custom",
	}
	if err := cfg.normalizeModels(); err != nil {
		t.Fatalf("normalizeModels() error: %v", err)
	}

	route, err := cfg.RouteModel("anthropic/claude-sonnet-4.6")
	if err != nil {
		t.Fatalf("RouteModel() error: %v", err)
	}
	if route.UpstreamModel != "sonnet-custom" {
		t.Fatalf("route.UpstreamModel = %q, want sonnet-custom", route.UpstreamModel)
	}

	route, err = cfg.RouteModel("openai/gpt-unknown")
	if err != nil {
		t.Fatalf("RouteModel() error: %v", err)
	}
	if route.UpstreamModel != "openai/gpt-unknown" {
		t.Fatalf("passthrough route.UpstreamModel = %q", route.UpstreamModel)
	}
}

func TestRouteModel_EmptyModelValue(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RawModels = map[string]any{
		"anthropic/claude-sonnet-4.6": "",
	}
	if err := cfg.normalizeModels(); err != nil {
		t.Fatalf("normalizeModels() error: %v", err)
	}

	route, err := cfg.RouteModel("anthropic/claude-sonnet-4.6")
	if err != nil {
		t.Fatalf("RouteModel() error: %v", err)
	}
	if route.UpstreamModel != "claude-sonnet-4.6" {
		t.Fatalf("route.UpstreamModel = %q, want claude-sonnet-4.6", route.UpstreamModel)
	}
}
