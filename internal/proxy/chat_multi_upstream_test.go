package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/zhangyc/trae-proxy/internal/config"
	"github.com/zhangyc/trae-proxy/internal/logging"
)

func testLogger() *logging.Logger {
	return logging.New(logging.LevelDebug, false, io.Discard)
}

func TestHandleChatCompletions_Anthropic(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"msg_1","content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":1,"output_tokens":2},"stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()

	cfg, err := config.Load("", map[string]string{"upstream": upstream.URL, "listen": ":443"})
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	s := NewServer(cfg, testLogger())

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"anthropic/claude-sonnet-4.6","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	HandleChatCompletions(s).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/messages" {
		t.Fatalf("upstream path = %q, want %q", gotPath, "/v1/messages")
	}
	if gotBody["model"] != "claude-sonnet-4.6" {
		t.Fatalf("upstream model = %#v, want %q", gotBody["model"], "claude-sonnet-4.6")
	}
	if !strings.Contains(rec.Body.String(), `"model":"anthropic/claude-sonnet-4.6"`) {
		t.Fatalf("response body missing original model: %s", rec.Body.String())
	}
}

func TestHandleChatCompletions_OpenAI(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"chatcmpl-1","object":"chat.completion","model":"gpt-4o","choices":[]}`)
	}))
	defer upstream.Close()

	tmp := t.TempDir()
	cfgPath := tmp + "/config.toml"
	if err := os.WriteFile(cfgPath, []byte(`
[upstreams.default]
url = "`+upstream.URL+`"
protocol = "openai"
default = true

[models]
"openai/gpt-5" = "gpt-4o"
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, err := config.Load(cfgPath, nil)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	s := NewServer(cfg, testLogger())

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"openai/gpt-5","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	HandleChatCompletions(s).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("upstream path = %q, want %q", gotPath, "/v1/chat/completions")
	}
	if gotBody["model"] != "gpt-4o" {
		t.Fatalf("upstream model = %#v, want %q", gotBody["model"], "gpt-4o")
	}
	if !strings.Contains(rec.Body.String(), `"model":"gpt-4o"`) {
		t.Fatalf("response body missing rewritten model: %s", rec.Body.String())
	}
}

func TestHandleChatCompletions_SSE(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		io.WriteString(w, "data: {\"id\":\"chunk-1\"}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	tmp := t.TempDir()
	cfgPath := tmp + "/config.toml"
	if err := os.WriteFile(cfgPath, []byte(`
[upstreams.default]
url = "`+upstream.URL+`"
protocol = "openai"
default = true
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, err := config.Load(cfgPath, nil)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	s := NewServer(cfg, testLogger())

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"openai/gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	HandleChatCompletions(s).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "data: {\"id\":\"chunk-1\"}") || !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("unexpected SSE body: %s", body)
	}
}

func TestClientFor_ReusesSameClient(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := tmp + "/config.toml"
	if err := os.WriteFile(cfgPath, []byte(`
[upstreams.default]
url = "https://api.example.com/v1"
protocol = "anthropic"
default = true

[upstreams.other]
url = "https://api.example.com/v2"
protocol = "openai"
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, err := config.Load(cfgPath, nil)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	s := NewServer(cfg, testLogger())

	a := s.clientFor(cfg.Upstreams["default"])
	b := s.clientFor(cfg.Upstreams["other"])
	if a != b {
		t.Fatal("expected same host to reuse the same http.Client instance")
	}
	if s.HTTPClient != a {
		t.Fatal("expected HTTPClient to match default upstream client")
	}
}
