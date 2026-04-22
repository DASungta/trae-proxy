package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// validLogLevels is the set of accepted log_level values.
var validLogLevels = map[string]bool{
	"trace": true, "debug": true, "info": true, "warn": true, "warning": true, "error": true,
}

type Upstream struct {
	URL      string `toml:"url"`
	Protocol string `toml:"protocol"` // "anthropic" | "openai"
	Default  bool   `toml:"default"`

	// internal cache (filled by normalize())
	upstreamOpenAIURL    string
	upstreamAnthropicURL string
	baseURL              string
}

type ModelRoute struct {
	Upstream string `toml:"upstream"`
	Model    string `toml:"model"` // empty = use fallback mapping
}

type ResolvedRoute struct {
	UpstreamModel string
	Upstream      *Upstream
}

// internal: normalized model route entry
type modelEntry struct {
	upstreamName string
	model        string // empty = fallback
}

type Config struct {
	// backward-compat fields (read from old config, not written to new config)
	Upstream         string `toml:"upstream"`
	UpstreamProtocol string `toml:"upstream_protocol"`

	// new fields
	Upstreams map[string]*Upstream `toml:"upstreams"`
	RawModels map[string]any       `toml:"models"` // TOML mixed-type decode

	Listen     string `toml:"listen"`
	Hijack     string `toml:"hijack"`
	RealModels bool   `toml:"real_models"`
	LogLevel   string `toml:"log_level"`
	LogBody    bool   `toml:"log_body"`

	// internal cache (filled after Load())
	defaultUpstream *Upstream
	resolvedModels  map[string]modelEntry
}

// ModelMapping is a single request-model → upstream-model pair.
type ModelMapping struct {
	RequestModel  string
	UpstreamModel string
}

// ModelGroup is a named group of model mappings used for ordered, annotated rendering.
type ModelGroup struct {
	Comment  string
	Mappings []ModelMapping
}

// DefaultModelGroups returns the canonical ordered list of model groups.
func DefaultModelGroups() []ModelGroup {
	return []ModelGroup{
		{
			Comment: "# 海外版（新模型）",
			Mappings: []ModelMapping{
				{"anthropic/claude-sonnet-4.6", "claude-sonnet-4.6"},
				{"anthropic/claude-opus-4.6", "claude-opus-4.6"},
				{"anthropic/claude-haiku-4.5", ""},
				{"openai/gpt-oss-120b", "gpt-5.4"},
				{"openai/gpt-5.4", ""},
				{"openai/gpt-5.4-mini", ""},
				{"google/gemini-3.1-pro-preview", ""},
				{"google/gemini-3.1-flash-lite-preview", ""},
				{"minimax/minimax-m2.7", ""},
				{"qwen/qwen3-coder-next", ""},
				{"z-ai/glm-5", ""},
			},
		},
		{
			Comment: "# 国内版（旧模型）",
			Mappings: []ModelMapping{
				{"anthropic/claude-sonnet-4.5", "claude-sonnet-4.6"},
				{"anthropic/claude-opus-4.1", "claude-opus-4.6"},
				{"anthropic/claude-4-sonnet", "claude-sonnet-4.6"},
				{"anthropic/claude-4-opus", "claude-opus-4.6"},
				{"anthropic/claude-3.7-sonnet", "claude-sonnet-4.6"},
				{"openai/gpt-5", "gpt-5.4"},
				{"openai/gpt-4.1", "gpt-5.4-mini"},
				{"openai/gpt-4o", "gpt-5.4-mini"},
				{"google/gemini-3-pro-preview", ""},
				{"google/gemini-2.5-pro", ""},
				{"minimax/minimax-m2", ""},
				{"qwen/qwen3-coder", ""},
			},
		},
	}
}

// DefaultModels returns the full default model map, merging any extra mappings.
func DefaultModels(extra map[string]string) map[string]string {
	m := make(map[string]string)
	for _, g := range DefaultModelGroups() {
		for _, mm := range g.Mappings {
			m[mm.RequestModel] = mm.UpstreamModel
		}
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func stringModelsToRaw(models map[string]string) map[string]any {
	raw := make(map[string]any, len(models))
	for k, v := range models {
		raw[k] = v
	}
	return raw
}

func DefaultConfig() *Config {
	models := DefaultModels(nil)
	cfg := &Config{
		Upstream:         "http://192.168.48.12:8080",
		UpstreamProtocol: "anthropic",
		Upstreams: map[string]*Upstream{
			"default": {
				URL:      "http://192.168.48.12:8080",
				Protocol: "anthropic",
				Default:  true,
			},
		},
		RawModels: stringModelsToRaw(models),
		Listen:    ":443",
		Hijack:    "openrouter.ai",
		LogLevel:  "info",
		LogBody:   false,
	}
	for _, upstream := range cfg.Upstreams {
		upstream.Protocol = normalizeProtocol(upstream.Protocol)
		upstream.normalize()
	}
	_ = cfg.resolveDefault()
	_ = cfg.normalizeModels()
	return cfg
}

func (u *Upstream) normalize() {
	raw := strings.TrimRight(strings.TrimSpace(u.URL), "/")
	u.URL = raw
	u.upstreamOpenAIURL = ""
	u.upstreamAnthropicURL = ""
	u.baseURL = raw
	switch {
	case strings.HasSuffix(raw, "/chat/completions"):
		u.upstreamOpenAIURL = raw
		u.baseURL = strings.TrimSuffix(raw, "/chat/completions")
	case strings.HasSuffix(raw, "/v1/messages"):
		u.upstreamAnthropicURL = raw
		u.baseURL = strings.TrimSuffix(raw, "/v1/messages")
	case strings.HasSuffix(raw, "/v1"):
		u.baseURL = strings.TrimSuffix(raw, "/v1")
	}
}

func (u *Upstream) ResolveURL(apiPath string) string {
	if strings.Contains(apiPath, "/messages") && u.upstreamAnthropicURL != "" {
		return u.upstreamAnthropicURL
	}
	if strings.Contains(apiPath, "/chat/completions") && u.upstreamOpenAIURL != "" {
		return u.upstreamOpenAIURL
	}
	return u.baseURL + apiPath
}

func normalizeProtocol(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func validateProtocol(v string) error {
	if v != "anthropic" && v != "openai" {
		return fmt.Errorf("invalid upstream_protocol %q (must be \"anthropic\" or \"openai\")", v)
	}
	return nil
}

func fallbackModelName(name string) string {
	if strings.HasPrefix(name, "anthropic/") {
		return strings.TrimPrefix(name, "anthropic/")
	}
	return name
}

func (c *Config) resolveDefault() error {
	var defaults []*Upstream
	for _, upstream := range c.Upstreams {
		if upstream.Default {
			defaults = append(defaults, upstream)
		}
	}
	if len(defaults) == 0 {
		return fmt.Errorf("no default upstream configured")
	}
	if len(defaults) > 1 {
		return fmt.Errorf("multiple default upstreams configured")
	}
	c.defaultUpstream = defaults[0]
	return nil
}

func (c *Config) normalizeModels() error {
	c.resolvedModels = make(map[string]modelEntry, len(c.RawModels))
	for k, v := range c.RawModels {
		switch val := v.(type) {
		case string:
			c.resolvedModels[k] = modelEntry{model: val}
		case map[string]any:
			upName, _ := val["upstream"].(string)
			model, _ := val["model"].(string)
			if upName != "" {
				if _, ok := c.Upstreams[upName]; !ok {
					return fmt.Errorf("[models] %q: unknown upstream %q", k, upName)
				}
			}
			c.resolvedModels[k] = modelEntry{upstreamName: upName, model: model}
		case *ModelRoute:
			if val == nil {
				return fmt.Errorf("[models] %q: unsupported nil value", k)
			}
			if val.Upstream != "" {
				if _, ok := c.Upstreams[val.Upstream]; !ok {
					return fmt.Errorf("[models] %q: unknown upstream %q", k, val.Upstream)
				}
			}
			c.resolvedModels[k] = modelEntry{upstreamName: val.Upstream, model: val.Model}
		default:
			return fmt.Errorf("[models] %q: unsupported value type %T", k, v)
		}
	}
	return nil
}

func (c *Config) resolveEntry(name string, entry modelEntry) (*ResolvedRoute, error) {
	upstream := c.defaultUpstream
	if entry.upstreamName != "" {
		var ok bool
		upstream, ok = c.Upstreams[entry.upstreamName]
		if !ok {
			return nil, fmt.Errorf("[models] %q: unknown upstream %q", name, entry.upstreamName)
		}
	}
	if upstream == nil {
		return nil, fmt.Errorf("default upstream is not configured")
	}
	model := entry.model
	if model == "" {
		model = fallbackModelName(name)
	}
	return &ResolvedRoute{
		UpstreamModel: model,
		Upstream:      upstream,
	}, nil
}

func (c *Config) RouteModel(name string) (*ResolvedRoute, error) {
	if entry, ok := c.resolvedModels[name]; ok {
		return c.resolveEntry(name, entry)
	}
	if strings.HasPrefix(name, "anthropic/") {
		trimmed := strings.TrimPrefix(name, "anthropic/")
		if entry, ok := c.resolvedModels[trimmed]; ok {
			return c.resolveEntry(trimmed, entry)
		}
	}
	if c.defaultUpstream == nil {
		return nil, fmt.Errorf("default upstream is not configured")
	}
	return &ResolvedRoute{
		UpstreamModel: fallbackModelName(name),
		Upstream:      c.defaultUpstream,
	}, nil
}

func Load(path string, overrides map[string]string) (*Config, error) {
	cfg := DefaultConfig()

	type fileConfig struct {
		Upstream         string               `toml:"upstream"`
		UpstreamProtocol string               `toml:"upstream_protocol"`
		Upstreams        map[string]*Upstream `toml:"upstreams"`
		RawModels        map[string]any       `toml:"models"`
		Listen           string               `toml:"listen"`
		Hijack           string               `toml:"hijack"`
		RealModels       bool                 `toml:"real_models"`
		LogLevel         string               `toml:"log_level"`
		LogBody          bool                 `toml:"log_body"`
	}

	if path != "" {
		if _, err := os.Stat(path); err == nil {
			var fc fileConfig
			if _, err := toml.DecodeFile(path, &fc); err != nil {
				return nil, err
			}
			if fc.Upstream != "" {
				cfg.Upstream = fc.Upstream
			}
			if fc.UpstreamProtocol != "" {
				cfg.UpstreamProtocol = fc.UpstreamProtocol
			}
			if len(fc.Upstreams) > 0 {
				cfg.Upstreams = fc.Upstreams
			} else if fc.Upstream != "" {
				cfg.Upstreams = nil
			}
			if len(fc.RawModels) > 0 {
				for k, v := range fc.RawModels {
					cfg.RawModels[k] = v
				}
			}
			if fc.Listen != "" {
				cfg.Listen = fc.Listen
			}
			if fc.Hijack != "" {
				cfg.Hijack = fc.Hijack
			}
			cfg.RealModels = fc.RealModels
			if fc.LogLevel != "" {
				cfg.LogLevel = fc.LogLevel
			}
			cfg.LogBody = fc.LogBody
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

	cfg.UpstreamProtocol = normalizeProtocol(cfg.UpstreamProtocol)
	if cfg.UpstreamProtocol == "" {
		cfg.UpstreamProtocol = "anthropic"
	}
	if err := validateProtocol(cfg.UpstreamProtocol); err != nil {
		return nil, err
	}

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

	cfg.LogLevel = strings.ToLower(strings.TrimSpace(cfg.LogLevel))
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if !validLogLevels[cfg.LogLevel] {
		return nil, fmt.Errorf("invalid log_level %q (must be trace/debug/info/warn/error)", cfg.LogLevel)
	}

	if len(cfg.Upstreams) == 0 && cfg.Upstream != "" {
		cfg.Upstreams = map[string]*Upstream{
			"default": {
				URL:      cfg.Upstream,
				Protocol: cfg.UpstreamProtocol,
				Default:  true,
			},
		}
	}

	for name, upstream := range cfg.Upstreams {
		if upstream == nil {
			return nil, fmt.Errorf("upstream %q is nil", name)
		}
		upstream.Protocol = normalizeProtocol(upstream.Protocol)
		if upstream.Protocol == "" {
			upstream.Protocol = "anthropic"
		}
		if err := validateProtocol(upstream.Protocol); err != nil {
			return nil, fmt.Errorf("upstream %q: %w", name, err)
		}
		upstream.normalize()
	}

	if err := cfg.resolveDefault(); err != nil {
		return nil, err
	}
	if v, ok := overrides["upstream"]; ok && v != "" {
		cfg.defaultUpstream.URL = strings.TrimRight(v, "/")
		cfg.defaultUpstream.normalize()
	}
	if err := cfg.normalizeModels(); err != nil {
		return nil, err
	}

	if cfg.defaultUpstream != nil {
		cfg.Upstream = cfg.defaultUpstream.URL
		cfg.UpstreamProtocol = cfg.defaultUpstream.Protocol
	}

	return cfg, nil
}

// Save writes the config to the given path in TOML format.
func Save(path string, cfg *Config) error {
	return SaveV3(path, cfg)
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

func (c *Config) DefaultUpstream() *Upstream {
	return c.defaultUpstream
}

func (c *Config) MapModel(name string) string {
	route, err := c.RouteModel(name)
	if err != nil {
		return fallbackModelName(name)
	}
	return route.UpstreamModel
}

func (c *Config) ModelIDs() []string {
	seen := make(map[string]bool)
	var ids []string
	source := c.resolvedModels
	if len(source) == 0 {
		source = make(map[string]modelEntry, len(c.RawModels))
		for k, v := range c.RawModels {
			switch val := v.(type) {
			case string:
				source[k] = modelEntry{model: val}
			case map[string]any:
				upName, _ := val["upstream"].(string)
				model, _ := val["model"].(string)
				source[k] = modelEntry{upstreamName: upName, model: model}
			}
		}
	}
	for k := range source {
		if !seen[k] {
			seen[k] = true
			ids = append(ids, k)
		}
	}
	sort.Strings(ids)
	return ids
}
