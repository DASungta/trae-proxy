package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

func HandleModels(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Config.RealModels {
			if forwardToRealModels(s, w, r) {
				return
			}
			// fallback to fake list on error
			s.Logger.Warn("real models fetch failed, falling back to config list")
		}
		serveFakeModels(s, w)
	}
}

// forwardToRealModels proxies GET /v1/models to the real hijack domain via
// BypassClient (which resolves DNS via 1.1.1.1, ignoring /etc/hosts).
// Returns true on success.
func forwardToRealModels(s *Server, w http.ResponseWriter, r *http.Request) bool {
	url := "https://" + s.Config.Hijack + "/api/v1/models"
	req, err := http.NewRequestWithContext(r.Context(), "GET", url, nil)
	if err != nil {
		s.Logger.Error("models build request error", "err", err)
		return false
	}
	// Forward Accept header; intentionally omit Authorization (public endpoint).
	if v := r.Header.Get("Accept"); v != "" {
		req.Header.Set("Accept", v)
	}

	resp, err := s.BypassClient.Do(req)
	if err != nil {
		s.Logger.Error("models upstream error", "err", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.Logger.Warn("models upstream non-200", "status", resp.StatusCode)
		return false
	}

	for k, vv := range resp.Header {
		lower := strings.ToLower(k)
		if lower == "transfer-encoding" || lower == "connection" || lower == "content-length" {
			continue
		}
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
	return true
}

func serveFakeModels(s *Server, w http.ResponseWriter) {
	ids := s.Config.ModelIDs()
	var models []map[string]interface{}
	now := int(time.Now().Unix())
	for _, id := range ids {
		tokenizer := "Other"
		switch {
		case strings.HasPrefix(id, "anthropic/"):
			tokenizer = "Claude"
		case strings.HasPrefix(id, "openai/"):
			tokenizer = "GPT"
		}

		models = append(models, map[string]interface{}{
			"id":             id,
			"canonical_slug": id,
			"name":           humanModelName(id),
			"created":        now,
			"description":    "",
			"context_length": 200000,
			"architecture": map[string]interface{}{
				"modality":          "text+image->text",
				"input_modalities":  []string{"text", "image"},
				"output_modalities": []string{"text"},
				"tokenizer":         tokenizer,
				"instruct_type":     nil,
			},
			"pricing": map[string]string{
				"prompt":     "0",
				"completion": "0",
			},
			"top_provider": map[string]interface{}{
				"context_length":        200000,
				"max_completion_tokens": 128000,
				"is_moderated":          false,
			},
			"per_request_limits": nil,
			"supported_parameters": []string{
				"max_tokens",
				"temperature",
				"tools",
				"tool_choice",
				"top_p",
				"response_format",
				"stop",
				"stream",
			},
			"default_parameters": map[string]interface{}{
				"temperature":       nil,
				"top_p":             nil,
				"top_k":             nil,
				"frequency_penalty": nil,
				"presence_penalty":  nil,
			},
			"knowledge_cutoff": nil,
			"expiration_date":  nil,
		})
	}
	resp := map[string]interface{}{
		"data": models,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func humanModelName(id string) string {
	parts := strings.SplitN(id, "/", 2)
	if len(parts) != 2 {
		return titleModelWords(id)
	}

	providerNames := map[string]string{
		"anthropic": "Anthropic",
		"openai":    "OpenAI",
		"google":    "Google",
		"minimax":   "MiniMax",
		"qwen":      "Qwen",
		"z-ai":      "Z-AI",
	}
	provider := providerNames[parts[0]]
	if provider == "" {
		provider = titleModelWords(parts[0])
	}
	return provider + ": " + titleModelWords(parts[1])
}

func titleModelWords(s string) string {
	words := strings.Split(strings.ReplaceAll(s, "-", " "), " ")
	for i, word := range words {
		switch strings.ToLower(word) {
		case "gpt":
			words[i] = "GPT"
		case "glm":
			words[i] = "GLM"
		case "claude":
			words[i] = "Claude"
		case "gemini":
			words[i] = "Gemini"
		case "minimax":
			words[i] = "MiniMax"
		case "qwen":
			words[i] = "Qwen"
		default:
			if word == "" {
				continue
			}
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	return strings.Join(words, " ")
}
