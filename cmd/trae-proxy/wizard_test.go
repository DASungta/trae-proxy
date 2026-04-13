package main

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestValidateUpstreamURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "valid http", input: "http://192.168.1.1:8080", want: "http://192.168.1.1:8080"},
		{name: "valid https", input: "https://api.example.com", want: "https://api.example.com"},
		{name: "trim trailing slash", input: "https://api.example.com/", want: "https://api.example.com"},
		{name: "trim whitespace", input: "  https://api.example.com  ", want: "https://api.example.com"},
		{name: "empty", input: "", wantErr: "不能为空"},
		{name: "no scheme", input: "api.example.com", wantErr: "http:// 或 https://"},
		{name: "contains /v1/messages", input: "https://api.example.com/v1/messages", want: "https://api.example.com/v1/messages"},
		{name: "contains /v1/chat/completions", input: "https://api.example.com/v1/chat/completions", want: "https://api.example.com/v1/chat/completions"},
		{name: "path ok", input: "https://api.example.com/api/maas", want: "https://api.example.com/api/maas"},
		{name: "Qianfan OpenAI full URL", input: "https://qianfan.baidubce.com/v2/coding/chat/completions", want: "https://qianfan.baidubce.com/v2/coding/chat/completions"},
		{name: "Qianfan Anthropic full URL", input: "https://qianfan.baidubce.com/anthropic/coding/v1/messages", want: "https://qianfan.baidubce.com/anthropic/coding/v1/messages"},
		{name: "Qianfan OpenAI base URL", input: "https://qianfan.baidubce.com/v2/coding", want: "https://qianfan.baidubce.com/v2/coding"},
		{name: "Qianfan Anthropic base URL", input: "https://qianfan.baidubce.com/anthropic/coding", want: "https://qianfan.baidubce.com/anthropic/coding"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateUpstreamURL(tt.input)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPromptUpstream(t *testing.T) {
	t.Run("valid input", func(t *testing.T) {
		in := strings.NewReader("https://api.example.com\n")
		var out bytes.Buffer
		scanner := newTestScanner(in)
		got, err := promptUpstream(scanner, &out)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "https://api.example.com" {
			t.Fatalf("got %q, want %q", got, "https://api.example.com")
		}
	})

	t.Run("invalid then valid", func(t *testing.T) {
		in := strings.NewReader("bad-url\nhttps://api.example.com\n")
		var out bytes.Buffer
		scanner := newTestScanner(in)
		got, err := promptUpstream(scanner, &out)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "https://api.example.com" {
			t.Fatalf("got %q, want %q", got, "https://api.example.com")
		}
		if !strings.Contains(out.String(), "✗") {
			t.Fatal("expected error indicator in output")
		}
	})
}

func TestPromptProtocol(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "default", input: "\n", want: "anthropic"},
		{name: "1", input: "1\n", want: "anthropic"},
		{name: "2", input: "2\n", want: "openai"},
		{name: "anthropic text", input: "anthropic\n", want: "anthropic"},
		{name: "openai text", input: "openai\n", want: "openai"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner := newTestScanner(strings.NewReader(tt.input))
			var out bytes.Buffer
			got, err := promptProtocol(scanner, &out)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPromptModel(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		scanner := newTestScanner(strings.NewReader("\n"))
		var out bytes.Buffer
		got, err := promptModel(scanner, &out)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != openRouterModels[0] {
			t.Fatalf("got %q, want %q", got, openRouterModels[0])
		}
	})

	t.Run("select number", func(t *testing.T) {
		scanner := newTestScanner(strings.NewReader("6\n"))
		var out bytes.Buffer
		got, err := promptModel(scanner, &out)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "openai/gpt-5" {
			t.Fatalf("got %q, want %q", got, "openai/gpt-5")
		}
	})

	t.Run("out of range then valid", func(t *testing.T) {
		scanner := newTestScanner(strings.NewReader("99\n3\n"))
		var out bytes.Buffer
		got, err := promptModel(scanner, &out)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "anthropic/claude-4-sonnet" {
			t.Fatalf("got %q, want %q", got, "anthropic/claude-4-sonnet")
		}
	})
}

func TestPromptUpstreamModel(t *testing.T) {
	t.Run("valid input", func(t *testing.T) {
		scanner := newTestScanner(strings.NewReader("claude-sonnet-4-6\n"))
		var out bytes.Buffer
		got, err := promptUpstreamModel(scanner, &out, "anthropic/claude-sonnet-4.5")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "claude-sonnet-4-6" {
			t.Fatalf("got %q, want %q", got, "claude-sonnet-4-6")
		}
	})

	t.Run("empty then valid", func(t *testing.T) {
		scanner := newTestScanner(strings.NewReader("\ngpt-4o\n"))
		var out bytes.Buffer
		got, err := promptUpstreamModel(scanner, &out, "openai/gpt-4o")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "gpt-4o" {
			t.Fatalf("got %q, want %q", got, "gpt-4o")
		}
	})
}

func TestWriteWizardConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	err := writeWizardConfig(path, "https://api.example.com", "openai", "openai/gpt-5", "gpt-4o")
	if err != nil {
		t.Fatalf("writeWizardConfig: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Verify we can parse the TOML.
	var parsed struct {
		Upstream         string            `toml:"upstream"`
		UpstreamProtocol string            `toml:"upstream_protocol"`
		Listen           string            `toml:"listen"`
		Hijack           string            `toml:"hijack"`
		Models           map[string]string `toml:"models"`
	}
	if _, err := toml.Decode(string(data), &parsed); err != nil {
		t.Fatalf("TOML decode: %v", err)
	}

	if parsed.Upstream != "https://api.example.com" {
		t.Errorf("upstream = %q, want %q", parsed.Upstream, "https://api.example.com")
	}
	if parsed.UpstreamProtocol != "openai" {
		t.Errorf("upstream_protocol = %q, want %q", parsed.UpstreamProtocol, "openai")
	}
	if parsed.Listen != ":443" {
		t.Errorf("listen = %q, want %q", parsed.Listen, ":443")
	}
	if parsed.Hijack != "openrouter.ai" {
		t.Errorf("hijack = %q, want %q", parsed.Hijack, "openrouter.ai")
	}
	if parsed.Models["openai/gpt-5"] != "gpt-4o" {
		t.Errorf("models[openai/gpt-5] = %q, want %q", parsed.Models["openai/gpt-5"], "gpt-4o")
	}
	// Other models should be empty string.
	if parsed.Models["anthropic/claude-sonnet-4.5"] != "" {
		t.Errorf("models[anthropic/claude-sonnet-4.5] = %q, want empty", parsed.Models["anthropic/claude-sonnet-4.5"])
	}
	if len(parsed.Models) != len(openRouterModels) {
		t.Errorf("model count = %d, want %d", len(parsed.Models), len(openRouterModels))
	}
}

func TestRunWizardE2E(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	// Simulate: upstream URL, protocol (default), model 1 (default), upstream model name.
	input := "https://api.example.com\n\n\nclaude-sonnet-4-6\n"
	var out bytes.Buffer

	err := runWizard(configPath, strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("runWizard: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "配置摘要") {
		t.Error("output missing 配置摘要")
	}
	if !strings.Contains(output, "claude-sonnet-4-6") {
		t.Error("output missing model mapping")
	}

	// Verify the file was created and is valid TOML.
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var parsed struct {
		Upstream         string            `toml:"upstream"`
		UpstreamProtocol string            `toml:"upstream_protocol"`
		Models           map[string]string `toml:"models"`
	}
	if _, err := toml.Decode(string(data), &parsed); err != nil {
		t.Fatalf("TOML decode: %v", err)
	}
	if parsed.Upstream != "https://api.example.com" {
		t.Errorf("upstream = %q", parsed.Upstream)
	}
	if parsed.UpstreamProtocol != "anthropic" {
		t.Errorf("upstream_protocol = %q", parsed.UpstreamProtocol)
	}
	if parsed.Models["anthropic/claude-sonnet-4.5"] != "claude-sonnet-4-6" {
		t.Errorf("models mapping = %q", parsed.Models["anthropic/claude-sonnet-4.5"])
	}
}

func newTestScanner(r io.Reader) *bufio.Scanner {
	return bufio.NewScanner(r)
}
