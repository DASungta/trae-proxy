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
