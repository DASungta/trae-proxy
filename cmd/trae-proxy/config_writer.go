package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/zhangyc/trae-proxy/internal/config"
)

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
	return fmt.Sprintf(`# trae-proxy configuration

# Upstream API address
# 上游服务地址，支持填写基础地址或完整端点 URL（两种方式均可）
# 示例：- 移动云：OpenAI：https://ai.bayesdl.com/api/maas/
# 示例：- 京东云：OpenAI：https://modelservice.jdcloud.com/coding/openai
# 示例：- 京东云：Anthropic：https://modelservice.jdcloud.com/coding/anthropic
# 示例：- 百度千帆：OpenAI：https://qianfan.baidubce.com/v2/coding
#                           或 https://qianfan.baidubce.com/v2/coding/chat/completions
# 示例：- 百度千帆：Anthropic：https://qianfan.baidubce.com/anthropic/coding
#                              或 https://qianfan.baidubce.com/anthropic/coding/v1/messages
# 示例：- sub2api：直接填端点地址
upstream = %q

# Upstream protocol: "anthropic" (default) performs OpenAI → Anthropic Messages
# conversion. "openai" directly forwards OpenAI Chat Completions — use this when
# upstream is OpenAI-compatible (LM Studio, Ollama, most relays).
# 上游服务是Anthropic协议填anthropic，如果是openai兼容填openai
upstream_protocol = %q

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

`, cfg.Upstream, cfg.UpstreamProtocol, cfg.Listen, cfg.Hijack)
}

func writeConfigFile(path string, cfg *config.Config) error {
	content := configHeader(cfg) + renderModelsSection(cfg.Models)
	return os.WriteFile(path, []byte(content), 0644)
}

func writeYuanxinConfig(path, protocol string) error {
	cfg := config.DefaultConfig()
	cfg.Upstream = "http://192.168.48.12:8080"
	cfg.UpstreamProtocol = protocol
	cfg.Models = config.DefaultModels(map[string]string{
		"yuanxin/claude-opus-4.7": "claude-opus-4-7",
		"yuanxin/gpt-5.4":        "gpt-5.4",
	})
	return writeConfigFile(path, cfg)
}
