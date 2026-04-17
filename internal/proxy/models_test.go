package proxy

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/zhangyc/trae-proxy/internal/config"
)

func TestServeFakeModelsOpenRouterFormat(t *testing.T) {
	s := &Server{
		Config: &config.Config{
			Models: map[string]string{
				"anthropic/claude-sonnet-4.5": "",
				"openai/gpt-5":                "",
			},
		},
	}

	rec := httptest.NewRecorder()
	serveFakeModels(s, rec)

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if _, ok := body["object"]; ok {
		t.Fatal("top-level object field should be absent")
	}

	data, ok := body["data"].([]interface{})
	if !ok || len(data) == 0 {
		t.Fatalf("data field missing or empty: %#v", body["data"])
	}

	for i, raw := range data {
		model, ok := raw.(map[string]interface{})
		if !ok {
			t.Fatalf("data[%d] has unexpected type %T", i, raw)
		}
		if _, ok := model["canonical_slug"]; !ok {
			t.Fatalf("data[%d] missing canonical_slug: %#v", i, model)
		}
	}
}

func TestServeFakeModelsDefaultConfigContainsBothGenerations(t *testing.T) {
	cfg := config.DefaultConfig()
	s := &Server{Config: cfg}

	rec := httptest.NewRecorder()
	serveFakeModels(s, rec)

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	data, ok := body["data"].([]interface{})
	if !ok {
		t.Fatalf("data field missing: %#v", body["data"])
	}

	ids := make(map[string]bool, len(data))
	for _, raw := range data {
		if m, ok := raw.(map[string]interface{}); ok {
			if id, ok := m["id"].(string); ok {
				ids[id] = true
			}
		}
	}

	// 新模型（海外版）
	mustContain := []string{
		"anthropic/claude-sonnet-4.6",
		"anthropic/claude-opus-4.6",
		"openai/gpt-5.4",
	}
	// 老模型（国内版）
	mustContain = append(mustContain,
		"anthropic/claude-sonnet-4.5",
		"anthropic/claude-3.7-sonnet",
		"google/gemini-3-pro-preview",
		"openai/gpt-5",
		"qwen/qwen3-coder",
	)

	for _, id := range mustContain {
		if !ids[id] {
			t.Errorf("missing expected model id %q in /v1/models response", id)
		}
	}

	if len(data) < 23 {
		t.Errorf("expected at least 23 models, got %d", len(data))
	}
}
