package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/zhangyc/trae-proxy/internal/config"
)

var forwardHeaders = []string{
	"Authorization", "Content-Type", "x-api-key",
	"anthropic-version", "anthropic-beta", "Accept",
}

var skipRespHeaders = map[string]bool{
	"transfer-encoding": true,
	"connection":        true,
	"content-length":    true,
}

func stripAPIPrefix(path string) string {
	if strings.HasPrefix(path, "/api/") {
		return path[4:]
	}
	if path == "/api" {
		return "/"
	}
	return path
}

func HandleForward(cfg *config.Config, client *http.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		upstreamPath := stripAPIPrefix(r.URL.RequestURI())

		ct := r.Header.Get("Content-Type")
		if strings.Contains(ct, "application/json") && len(body) > 0 {
			var data map[string]interface{}
			if err := json.Unmarshal(body, &data); err == nil {
				if model, ok := data["model"].(string); ok {
					mapped := cfg.MapModel(model)
					if mapped != model {
						data["model"] = mapped
						body, _ = json.Marshal(data)
					}
				}
			}
		}

		url := cfg.Upstream + upstreamPath
		req, err := http.NewRequest(r.Method, url, strings.NewReader(string(body)))
		if err != nil {
			sendProxyError(w, http.StatusBadGateway, fmt.Sprintf("failed to create request: %v", err))
			return
		}

		for _, h := range forwardHeaders {
			if v := r.Header.Get(h); v != "" {
				req.Header.Set(h, v)
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			sendProxyError(w, http.StatusBadGateway, fmt.Sprintf("upstream unreachable: %s", cfg.Upstream))
			return
		}
		defer resp.Body.Close()

		for k, vv := range resp.Header {
			if !skipRespHeaders[strings.ToLower(k)] {
				for _, v := range vv {
					w.Header().Add(k, v)
				}
			}
		}
		w.WriteHeader(resp.StatusCode)

		buf := make([]byte, 4096)
		flusher, canFlush := w.(http.Flusher)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				w.Write(buf[:n])
				if canFlush {
					flusher.Flush()
				}
			}
			if err != nil {
				break
			}
		}
	}
}

func sendProxyError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"message": msg,
			"type":    "proxy_error",
		},
	})
}
