package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Upstream         string            `toml:"upstream"`
	UpstreamProtocol string            `toml:"upstream_protocol"` // "anthropic" (default) | "openai"
	Listen           string            `toml:"listen"`
	Hijack           string            `toml:"hijack"`
	Models           map[string]string `toml:"models"`
	RealModels       bool              `toml:"real_models"`
}

func DefaultConfig() *Config {
	return &Config{
		Upstream:         "http://192.168.48.12:8080",
		UpstreamProtocol: "anthropic",
		Listen:           ":443",
		Hijack:           "openrouter.ai",
		Models: map[string]string{
			"anthropic/claude-sonnet-4.6": "claude-sonnet-4-6",
			"anthropic/claude-sonnet-4-6": "claude-sonnet-4-6",
			"anthropic/claude-sonnet-4.5": "claude-sonnet-4-5-20251001",
			"anthropic/claude-sonnet-4-5": "claude-sonnet-4-5-20251001",
			"anthropic/claude-haiku-4.5":  "claude-haiku-4-5-20251001",
			"anthropic/claude-haiku-4-5":  "claude-haiku-4-5-20251001",
			"anthropic/claude-opus-4.6":   "claude-opus-4-6",
			"anthropic/claude-opus-4-6":   "claude-opus-4-6",
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

	cfg.UpstreamProtocol = strings.ToLower(strings.TrimSpace(cfg.UpstreamProtocol))
	if cfg.UpstreamProtocol == "" {
		cfg.UpstreamProtocol = "anthropic"
	}
	if cfg.UpstreamProtocol != "anthropic" && cfg.UpstreamProtocol != "openai" {
		return nil, fmt.Errorf("invalid upstream_protocol %q (must be \"anthropic\" or \"openai\")", cfg.UpstreamProtocol)
	}

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
