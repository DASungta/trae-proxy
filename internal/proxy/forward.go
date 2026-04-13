package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zhangyc/trae-proxy/internal/config"
	"github.com/zhangyc/trae-proxy/internal/logging"
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

func HandleForward(cfg *config.Config, logger *logging.Logger, client *http.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var (
			status   int = 200
			bytesOut int64
		)
		defer func() {
			logger.Info("request done",
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"dur_ms", time.Since(start).Milliseconds(),
				"bytes_out", bytesOut,
			)
		}()

		body, err := io.ReadAll(r.Body)
		if err != nil {
			status = http.StatusBadRequest
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Tap 1: original client request.
		logger.Trace("client request",
			"method", r.Method, "path", r.URL.Path,
			"headers", logging.RedactHeaders(r.Header),
			"body_size", len(body),
			"body", bodyAttr(logger, body),
		)

		upstreamPath := stripAPIPrefix(r.URL.RequestURI())

		ct := r.Header.Get("Content-Type")
		if strings.Contains(ct, "application/json") && len(body) > 0 {
			var data map[string]interface{}
			if err := json.Unmarshal(body, &data); err == nil {
				if model, ok := data["model"].(string); ok {
					mapped := cfg.MapModel(model)
					if mapped != model {
						logger.Debug("model rewritten", "from", model, "to", mapped)
						data["model"] = mapped
						body, _ = json.Marshal(data)
						// Tap 2: body after model rewrite.
						logger.Trace("proxy rewritten body",
							"body_size", len(body),
							"body", bodyAttr(logger, body),
						)
					}
				}
			}
		}

		var url string
		if strings.HasSuffix(upstreamPath, "/chat/completions") {
			url = cfg.ResolveUpstreamURL(upstreamPath)
		} else {
			url = cfg.Upstream + upstreamPath
		}
		req, err := http.NewRequestWithContext(r.Context(), r.Method, url, strings.NewReader(string(body)))
		if err != nil {
			status = http.StatusBadGateway
			sendProxyError(w, http.StatusBadGateway, fmt.Sprintf("failed to create request: %v", err))
			return
		}

		for _, h := range forwardHeaders {
			if v := r.Header.Get(h); v != "" {
				req.Header.Set(h, v)
			}
		}

		// Tap 3: upstream request.
		logger.Trace("upstream request",
			"url", url,
			"method", r.Method,
			"headers", logging.RedactHeaders(req.Header),
			"body_size", len(body),
			"body", bodyAttr(logger, body),
		)

		resp, err := client.Do(req)
		if err != nil {
			status = http.StatusBadGateway
			sendProxyError(w, http.StatusBadGateway, fmt.Sprintf("upstream unreachable: %s", cfg.Upstream))
			return
		}
		defer resp.Body.Close()

		status = resp.StatusCode

		for k, vv := range resp.Header {
			if !skipRespHeaders[strings.ToLower(k)] {
				for _, v := range vv {
					w.Header().Add(k, v)
				}
			}
		}
		w.WriteHeader(resp.StatusCode)

		// Tap 4: stream upstream response into client, tee into trace buffer.
		buf := make([]byte, 4096)
		flusher, canFlush := w.(http.Flusher)
		var totalN int64

		var traceBuf bytes.Buffer
		collectTrace := logger.Enabled(logging.LevelTrace)

		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				totalN += int64(n)
				if _, writeErr := w.Write(buf[:n]); writeErr != nil {
					break // client disconnected
				}
				bytesOut += int64(n)
				if canFlush {
					flusher.Flush()
				}
				if collectTrace {
					logging.AppendCapped(&traceBuf, buf[:n], traceCap)
				}
			}
			if readErr != nil {
				break
			}
		}

		if collectTrace {
			logger.Trace("upstream response",
				"status", resp.StatusCode,
				"body_size", totalN,
				"body", bodyAttr(logger, traceBuf.Bytes()),
			)
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
