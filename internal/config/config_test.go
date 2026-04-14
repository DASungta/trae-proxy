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

func TestResolveUpstreamURL(t *testing.T) {
	tests := []struct {
		name     string
		upstream string
		apiPath  string
		wantURL  string
		wantBase string // expected cfg.Upstream after parseUpstreamURL
	}{
		{
			name:     "base URL + /v1/messages",
			upstream: "https://api.example.com",
			apiPath:  "/v1/messages",
			wantURL:  "https://api.example.com/v1/messages",
			wantBase: "https://api.example.com",
		},
		{
			name:     "base URL + /v1/chat/completions",
			upstream: "https://api.example.com",
			apiPath:  "/v1/chat/completions",
			wantURL:  "https://api.example.com/v1/chat/completions",
			wantBase: "https://api.example.com",
		},
		{
			name:     "Qianfan Anthropic full URL resolves messages path",
			upstream: "https://qianfan.baidubce.com/anthropic/coding/v1/messages",
			apiPath:  "/v1/messages",
			wantURL:  "https://qianfan.baidubce.com/anthropic/coding/v1/messages",
			wantBase: "https://qianfan.baidubce.com/anthropic/coding",
		},
		{
			name:     "Qianfan OpenAI full URL resolves chat/completions path",
			upstream: "https://qianfan.baidubce.com/v2/coding/chat/completions",
			apiPath:  "/v1/chat/completions",
			wantURL:  "https://qianfan.baidubce.com/v2/coding/chat/completions",
			wantBase: "https://qianfan.baidubce.com/v2/coding",
		},
		{
			name:     "Qianfan OpenAI base URL appends /v1/chat/completions",
			upstream: "https://qianfan.baidubce.com/v2/coding",
			apiPath:  "/v1/chat/completions",
			wantURL:  "https://qianfan.baidubce.com/v2/coding/v1/chat/completions",
			wantBase: "https://qianfan.baidubce.com/v2/coding",
		},
		{
			name:     "standard full /v1/chat/completions URL",
			upstream: "https://api.example.com/v1/chat/completions",
			apiPath:  "/v1/chat/completions",
			wantURL:  "https://api.example.com/v1/chat/completions",
			wantBase: "https://api.example.com/v1",
		},
		{
			name:     "Anthropic full URL does not match chat/completions path",
			upstream: "https://qianfan.baidubce.com/anthropic/coding/v1/messages",
			apiPath:  "/v1/chat/completions",
			wantURL:  "https://qianfan.baidubce.com/anthropic/coding/v1/chat/completions",
			wantBase: "https://qianfan.baidubce.com/anthropic/coding",
		},
		{
			name:     "trailing slash is normalized",
			upstream: "https://api.example.com/",
			apiPath:  "/v1/messages",
			wantURL:  "https://api.example.com/v1/messages",
			wantBase: "https://api.example.com",
		},
		{
			name:     "/v1/ suffix: chat/completions path",
			upstream: "https://coding.dashscope.aliyuncs.com/v1/",
			apiPath:  "/v1/chat/completions",
			wantURL:  "https://coding.dashscope.aliyuncs.com/v1/chat/completions",
			wantBase: "https://coding.dashscope.aliyuncs.com",
		},
		{
			name:     "/v1/ suffix: messages path",
			upstream: "https://coding.dashscope.aliyuncs.com/v1/",
			apiPath:  "/v1/messages",
			wantURL:  "https://coding.dashscope.aliyuncs.com/v1/messages",
			wantBase: "https://coding.dashscope.aliyuncs.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Upstream: tt.upstream}
			cfg.parseUpstreamURL()

			if cfg.Upstream != tt.wantBase {
				t.Errorf("cfg.Upstream = %q, want %q", cfg.Upstream, tt.wantBase)
			}

			got := cfg.ResolveUpstreamURL(tt.apiPath)
			if got != tt.wantURL {
				t.Errorf("ResolveUpstreamURL(%q) = %q, want %q", tt.apiPath, got, tt.wantURL)
			}
		})
	}
}
