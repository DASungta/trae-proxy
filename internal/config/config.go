package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// validLogLevels is the set of accepted log_level values.
var validLogLevels = map[string]bool{
	"trace": true, "debug": true, "info": true, "warn": true, "warning": true, "error": true,
}

type Config struct {
	Upstream         string            `toml:"upstream"`
	UpstreamProtocol string            `toml:"upstream_protocol"` // "anthropic" (default) | "openai"
	Listen           string            `toml:"listen"`
	Hijack           string            `toml:"hijack"`
	Models           map[string]string `toml:"models"`
	RealModels       bool              `toml:"real_models"`
	LogLevel         string            `toml:"log_level"` // trace|debug|info|warn|error (default: info)
	LogBody          bool              `toml:"log_body"`  // print full request/response bodies at trace level

	// upstreamOpenAIURL holds the full OpenAI endpoint URL when the user supplies one
	// (e.g. https://qianfan.baidubce.com/v2/coding/chat/completions).
	// Empty string means "derive from Upstream base + api path".
	upstreamOpenAIURL string
	// upstreamAnthropicURL holds the full Anthropic endpoint URL when the user supplies one
	// (e.g. https://qianfan.baidubce.com/anthropic/coding/v1/messages).
	upstreamAnthropicURL string
}

func DefaultConfig() *Config {
	return &Config{
		Upstream:         "http://192.168.48.12:8080",
		UpstreamProtocol: "anthropic",
		Listen:           ":443",
		Hijack:           "openrouter.ai",
		LogLevel:         "info",
		LogBody:          false,
		Models: map[string]string{
			"anthropic/claude-sonnet-4.5":  "claude-sonnet-4.6",
			"anthropic/claude-opus-4.1":    "claude-opus-4.6",
			"anthropic/claude-4-sonnet":    "",
			"anthropic/claude-4-opus":      "",
			"anthropic/claude-3.7-sonnet":  "",
			"openai/gpt-5":                "gpt-5.4",
			"openai/gpt-4.1":              "",
			"openai/gpt-4o":               "",
			"google/gemini-3-pro-perview":  "",
			"google/gemini-2.5-pro":        "",
			"minimax/minimax-m2":           "",
			"qwen/qwen3-coder":            "",
		},
	}
}

func Load(path string, overrides map[string]string) (*Config, error) {
	cfg := DefaultConfig()

	if path != "" {
		if _, err := os.Stat(path); err == nil {
			if _, err := toml.DecodeFile(path, cfg); err != nil {
				return nil, err
			}
		}
	}

	if v, ok := overrides["upstream"]; ok && v != "" {
		cfg.Upstream = v
	}
	if v, ok := overrides["listen"]; ok && v != "" {
		cfg.Listen = v
	}
	if v, ok := overrides["hijack"]; ok && v != "" {
		cfg.Hijack = v
	}
	if v, ok := overrides["log_level"]; ok && v != "" {
		cfg.LogLevel = v
	}
	if v, ok := overrides["log_body"]; ok {
		cfg.LogBody = v == "true" || v == "1"
	}

	cfg.UpstreamProtocol = strings.ToLower(strings.TrimSpace(cfg.UpstreamProtocol))
	if cfg.UpstreamProtocol == "" {
		cfg.UpstreamProtocol = "anthropic"
	}
	if cfg.UpstreamProtocol != "anthropic" && cfg.UpstreamProtocol != "openai" {
		return nil, fmt.Errorf("invalid upstream_protocol %q (must be \"anthropic\" or \"openai\")", cfg.UpstreamProtocol)
	}

	// Apply environment variable fallbacks (lower priority than CLI overrides).
	if _, ok := overrides["log_level"]; !ok {
		if v := os.Getenv("TRAE_LOG_LEVEL"); v != "" {
			cfg.LogLevel = v
		}
	}
	if _, ok := overrides["log_body"]; !ok {
		if v := os.Getenv("TRAE_LOG_BODY"); v == "true" || v == "1" {
			cfg.LogBody = true
		}
	}

	// Validate log level.
	cfg.LogLevel = strings.ToLower(strings.TrimSpace(cfg.LogLevel))
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if !validLogLevels[cfg.LogLevel] {
		return nil, fmt.Errorf("invalid log_level %q (must be trace/debug/info/warn/error)", cfg.LogLevel)
	}

	cfg.parseUpstreamURL()

	return cfg, nil
}

func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "trae-proxy")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

func (c *Config) MapModel(name string) string {
	if mapped, ok := c.Models[name]; ok {
		return mapped
	}
	if strings.HasPrefix(name, "anthropic/") {
		return strings.TrimPrefix(name, "anthropic/")
	}
	return name
}

func (c *Config) ModelIDs() []string {
	seen := make(map[string]bool)
	var ids []string
	for k := range c.Models {
		if !seen[k] {
			seen[k] = true
			ids = append(ids, k)
		}
	}
	return ids
}

// parseUpstreamURL inspects cfg.Upstream for a full endpoint URL suffix.
// When a known suffix is found, it is stored in the corresponding private field
// and Upstream is trimmed to the base URL.
// Supported suffixes:
//   - /chat/completions   → OpenAI full URL (e.g. Qianfan /v2/coding/chat/completions)
//   - /v1/chat/completions → OpenAI standard full URL
//   - /v1/messages        → Anthropic full URL
func (c *Config) parseUpstreamURL() {
	raw := strings.TrimRight(c.Upstream, "/")
	switch {
	case strings.HasSuffix(raw, "/chat/completions"):
		c.upstreamOpenAIURL = raw
		c.Upstream = strings.TrimSuffix(raw, "/chat/completions")
	case strings.HasSuffix(raw, "/v1/messages"):
		c.upstreamAnthropicURL = raw
		c.Upstream = strings.TrimSuffix(raw, "/v1/messages")
	case strings.HasSuffix(raw, "/v1"):
		// User provided e.g. "https://host/v1/" — strip /v1 since apiPath already includes it.
		c.Upstream = strings.TrimSuffix(raw, "/v1")
	default:
		c.Upstream = raw // normalize: strip trailing slash
	}
}

// ResolveUpstreamURL returns the full request URL for the given API path.
// apiPath is the internal path such as "/v1/messages" or "/v1/chat/completions".
// When the user supplied a full endpoint URL for the matching protocol, that URL
// is returned directly (ignoring apiPath). Otherwise the base Upstream is concatenated
// with apiPath, preserving existing behaviour for all non-Qianfan upstreams.
func (c *Config) ResolveUpstreamURL(apiPath string) string {
	if strings.Contains(apiPath, "/messages") && c.upstreamAnthropicURL != "" {
		return c.upstreamAnthropicURL
	}
	if strings.Contains(apiPath, "/chat/completions") && c.upstreamOpenAIURL != "" {
		return c.upstreamOpenAIURL
	}
	return c.Upstream + apiPath
}
