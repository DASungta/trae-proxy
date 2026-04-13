package main

import (
	"bufio"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// openRouterModels lists the models Trae currently exposes via OpenRouter.
var openRouterModels = []string{
	"anthropic/claude-sonnet-4.5",
	"anthropic/claude-opus-4.1",
	"anthropic/claude-4-sonnet",
	"anthropic/claude-4-opus",
	"anthropic/claude-3.7-sonnet",
	"openai/gpt-5",
	"openai/gpt-4.1",
	"openai/gpt-4o",
	"google/gemini-3-pro-perview",
	"google/gemini-2.5-pro",
	"minimax/minimax-m2",
	"qwen/qwen3-coder",
}

// validateUpstreamURL validates and normalises an upstream URL.
func validateUpstreamURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("地址不能为空")
	}

	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return "", fmt.Errorf("地址必须以 http:// 或 https:// 开头")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("URL 格式无效: %v", err)
	}

	path := strings.TrimRight(u.Path, "/")
	if strings.HasSuffix(path, "/v1/messages") {
		u.Path = strings.TrimSuffix(path, "/v1/messages")
		return "", fmt.Errorf("地址不要包含 /v1/messages，请填写基础地址: %s", u.String())
	}
	if strings.HasSuffix(path, "/v1/chat/completions") {
		u.Path = strings.TrimSuffix(path, "/v1/chat/completions")
		return "", fmt.Errorf("地址不要包含 /v1/chat/completions，请填写基础地址: %s", u.String())
	}

	// Trim trailing slash for consistency.
	result := strings.TrimRight(raw, "/")
	return result, nil
}

func promptUpstream(scanner *bufio.Scanner, out io.Writer) (string, error) {
	fmt.Fprintln(out, "--- Step 1/3: 上游服务地址 ---")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "请输入上游 API 地址（只填基础地址，不要包含 /v1/messages 或 /v1/chat/completions）")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "示例：")
	fmt.Fprintln(out, "  移动云:            https://ai.bayesdl.com/api/maas/")
	fmt.Fprintln(out, "  京东云(OpenAI):     https://modelservice.jdcloud.com/coding/openai")
	fmt.Fprintln(out, "  京东云(Anthropic):  https://modelservice.jdcloud.com/coding/anthropic")
	fmt.Fprintln(out, "  sub2api:           http://your-server:8080")
	fmt.Fprintln(out)

	for {
		fmt.Fprint(out, "上游地址: ")
		if !scanner.Scan() {
			return "", io.EOF
		}
		input := scanner.Text()
		result, err := validateUpstreamURL(input)
		if err != nil {
			fmt.Fprintf(out, "  ✗ %s\n\n", err)
			continue
		}
		return result, nil
	}
}

func promptProtocol(scanner *bufio.Scanner, out io.Writer) (string, error) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, "--- Step 2/3: 上游协议 ---")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "你的上游服务使用哪种协议？")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "  1) anthropic  - 上游接受 Anthropic Messages API")
	fmt.Fprintln(out, "                  trae-proxy 会自动将 OpenAI 格式转换为 Anthropic 格式")
	fmt.Fprintln(out, "                  适用于：原生 Anthropic 端点、部分云服务商")
	fmt.Fprintln(out, "  2) openai     - 上游接受 OpenAI Chat Completions API")
	fmt.Fprintln(out, "                  trae-proxy 直接转发（仅重写模型名称）")
	fmt.Fprintln(out, "                  适用于：中转站、LM Studio、Ollama")
	fmt.Fprintln(out)

	for {
		fmt.Fprint(out, "协议 [1]: ")
		if !scanner.Scan() {
			return "", io.EOF
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" || input == "1" || input == "anthropic" {
			return "anthropic", nil
		}
		if input == "2" || input == "openai" {
			return "openai", nil
		}
		fmt.Fprintln(out, "  ✗ 请输入 1 或 2")
	}
}

func promptModel(scanner *bufio.Scanner, out io.Writer) (string, error) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, "--- Step 3/3: 模型映射 ---")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "在 Trae 中选择以下任一模型时，请求会被映射到你指定的上游模型。")
	fmt.Fprintln(out)
	for i, m := range openRouterModels {
		fmt.Fprintf(out, "  %2d) %s\n", i+1, m)
	}
	fmt.Fprintln(out)

	for {
		fmt.Fprint(out, "选择要映射的模型 [1]: ")
		if !scanner.Scan() {
			return "", io.EOF
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			return openRouterModels[0], nil
		}
		n, err := strconv.Atoi(input)
		if err != nil || n < 1 || n > len(openRouterModels) {
			fmt.Fprintf(out, "  ✗ 请输入 1-%d 之间的数字\n", len(openRouterModels))
			continue
		}
		return openRouterModels[n-1], nil
	}
}

func promptUpstreamModel(scanner *bufio.Scanner, out io.Writer, selectedModel string) (string, error) {
	fmt.Fprintln(out)
	fmt.Fprintf(out, "请输入上游服务实际接受的模型名称（当 Trae 请求 %s 时，将发送此名称给上游）\n", selectedModel)
	fmt.Fprintln(out, "例如: claude-sonnet-4-6, gpt-4o, glm-4-plus")
	fmt.Fprintln(out)

	for {
		fmt.Fprint(out, "上游模型名称: ")
		if !scanner.Scan() {
			return "", io.EOF
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			fmt.Fprintln(out, "  ✗ 模型名称不能为空")
			continue
		}
		return input, nil
	}
}

func writeWizardConfig(path, upstream, protocol, selectedModel, upstreamModel string) error {
	var b strings.Builder
	b.WriteString(`# trae-proxy configuration

# Upstream API address
# 上游服务地址，路径不要包含/v1/messages或/v1/chat/completions
# 示例：- 移动云：OpenAI：https://ai.bayesdl.com/api/maas/
# 示例：- 京东云：OpenAI：https://modelservice.jdcloud.com/coding/openai
# 示例：- 京东云：Anthropic：https://modelservice.jdcloud.com/coding/anthropic
# 示例：- sub2api：直接填端点地址
`)
	fmt.Fprintf(&b, "upstream = %q\n", upstream)
	b.WriteString(`
# Upstream protocol: "anthropic" (default) performs OpenAI → Anthropic Messages
# conversion. "openai" directly forwards OpenAI Chat Completions — use this when
# upstream is OpenAI-compatible (LM Studio, Ollama, most relays).
# 上游服务是Anthropic协议填anthropic，如果是openai兼容填openai
`)
	fmt.Fprintf(&b, "upstream_protocol = %q\n", protocol)
	b.WriteString(`
# HTTPS listen address
listen = ":443"

# Domain to hijack via /etc/hosts
hijack = "openrouter.ai"

# Log level: error | warn | info (default) | debug | trace
log_level = "info"

# When true, the trace level prints full request/response bodies.
log_body = false

# When true, GET /v1/models forwards to the real hijack domain (bypassing /etc/hosts)
# instead of returning the fake list from [models] below.
# real_models = false

# Model mapping: request model name → upstream model name
# 3-tier fallback: exact match → strip "anthropic/" prefix → passthrough
# 如果劫持openrouter，模型名称是有"anthropic/"、"openai/"等前缀
# 以下是当前Trae中OpenRouter列出的模型，任选一个将请求模型映射到上游服务提供的真实模型
[models]
`)
	for _, m := range openRouterModels {
		if m == selectedModel {
			fmt.Fprintf(&b, "%q = %q\n", m, upstreamModel)
		} else {
			fmt.Fprintf(&b, "%q = \"\"\n", m)
		}
	}

	return os.WriteFile(path, []byte(b.String()), 0644)
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func runWizard(configPath string, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)

	fmt.Fprintln(out, "=== trae-proxy 初始化向导 ===")
	fmt.Fprintln(out, "你可以随时在配置文件中修改这些配置。")
	fmt.Fprintln(out)

	upstream, err := promptUpstream(scanner, out)
	if err != nil {
		return err
	}

	protocol, err := promptProtocol(scanner, out)
	if err != nil {
		return err
	}

	selectedModel, err := promptModel(scanner, out)
	if err != nil {
		return err
	}

	upstreamModel, err := promptUpstreamModel(scanner, out, selectedModel)
	if err != nil {
		return err
	}

	// Print summary.
	fmt.Fprintln(out)
	fmt.Fprintln(out, "=== 配置摘要 ===")
	fmt.Fprintf(out, "  上游地址:  %s\n", upstream)
	fmt.Fprintf(out, "  协议:      %s\n", protocol)
	fmt.Fprintf(out, "  模型映射:  %s → %s\n", selectedModel, upstreamModel)
	fmt.Fprintf(out, "  配置文件:  %s\n", configPath)
	fmt.Fprintln(out)

	if err := writeWizardConfig(configPath, upstream, protocol, selectedModel, upstreamModel); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}

	fmt.Fprintf(out, "[init] 配置已写入 %s\n", configPath)
	return nil
}
