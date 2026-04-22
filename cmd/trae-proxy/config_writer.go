package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/zhangyc/trae-proxy/internal/config"
)

func rawStringModels(raw map[string]any) map[string]string {
	models := make(map[string]string, len(raw))
	for k, v := range raw {
		if s, ok := v.(string); ok {
			models[k] = s
		}
	}
	return models
}

func renderModelsSection(models map[string]string) string {
	var b strings.Builder
	b.WriteString(`# Model mapping: request model name → upstream model name
# 3-tier fallback: exact match → strip "anthropic/" prefix → passthrough
# 如果劫持openrouter，模型名称是有"anthropic/"、"openai/"等前缀
# 以下是当前Trae中OpenRouter列出的模型，任选一个将请求模型映射到上游服务提供的真实模型
[models]
`)

	seen := make(map[string]bool)
	for _, group := range config.DefaultModelGroups() {
		b.WriteString(group.Comment + "\n")
		for _, mm := range group.Mappings {
			seen[mm.RequestModel] = true
			fmt.Fprintf(&b, "%q = %q\n", mm.RequestModel, models[mm.RequestModel])
		}
		b.WriteString("\n")
	}

	var extras []string
	for k := range models {
		if !seen[k] {
			extras = append(extras, k)
		}
	}
	sort.Strings(extras)
	for _, k := range extras {
		fmt.Fprintf(&b, "%q = %q\n", k, models[k])
	}
	if len(extras) > 0 {
		b.WriteString("\n")
	}
	return b.String()
}

func configHeader(cfg *config.Config) string {
	upstream := cfg.Upstreams["default"]
	if upstream == nil {
		upstream = cfg.DefaultUpstream()
	}
	if upstream == nil {
		upstream = &config.Upstream{
			URL:      cfg.Upstream,
			Protocol: cfg.UpstreamProtocol,
			Default:  true,
		}
	}
	return fmt.Sprintf(`# trae-proxy configuration

# HTTPS listen address
listen = %q

# Domain to hijack via /etc/hosts
hijack = %q

# Log level: error | warn | info (default) | debug | trace
log_level = "info"

# When true, the trace level prints full request/response bodies.
log_body = false

# When true, GET /v1/models forwards to the real hijack domain (bypassing /etc/hosts)
# instead of returning the fake list from [models] below.
# real_models = false

# Upstream config (supports multiple named upstreams)
[upstreams.default]
url = %q
protocol = %q
default = true

# Multi-upstream example (add manually):
# [upstreams.another]
# url = "https://api.openai.com"
# protocol = "openai"

`, cfg.Listen, cfg.Hijack, upstream.URL, upstream.Protocol)
}

func writeConfigFile(path string, cfg *config.Config) error {
	content := configHeader(cfg) + renderModelsSection(rawStringModels(cfg.RawModels))
	return os.WriteFile(path, []byte(content), 0644)
}

func writeYuanxinConfig(path, protocol string) error {
	cfg := config.DefaultConfig()
	cfg.Upstream = "http://192.168.48.12:8080"
	cfg.UpstreamProtocol = protocol
	cfg.Upstreams = map[string]*config.Upstream{
		"default": {
			URL:      cfg.Upstream,
			Protocol: protocol,
			Default:  true,
		},
	}
	cfg.RawModels = map[string]any{}
	for k, v := range config.DefaultModels(map[string]string{
		"yuanxin/claude-opus-4.7": "claude-opus-4-7",
		"yuanxin/gpt-5.4":         "gpt-5.4",
	}) {
		cfg.RawModels[k] = v
	}
	return writeConfigFile(path, cfg)
}
