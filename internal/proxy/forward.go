package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

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

func doForwardRequest(logger *logging.Logger, client *http.Client, w http.ResponseWriter, r *http.Request, upstreamURL string, body []byte) (int, int64) {
	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, strings.NewReader(string(body)))
	if err != nil {
		sendProxyError(w, http.StatusBadGateway, fmt.Sprintf("failed to create request: %v", err))
		return http.StatusBadGateway, 0
	}

	for _, h := range forwardHeaders {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}

	logger.Trace("upstream request",
		"url", upstreamURL,
		"method", r.Method,
		"headers", logging.RedactHeaders(req.Header),
		"body_size", len(body),
		"body", bodyAttr(logger, body),
	)

	resp, err := client.Do(req)
	if err != nil {
		sendProxyError(w, http.StatusBadGateway, fmt.Sprintf("upstream unreachable: %s", upstreamURL))
		return http.StatusBadGateway, 0
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
	var totalN int64
	var bytesOut int64
	var traceBuf bytes.Buffer
	collectTrace := logger.Enabled(logging.LevelTrace)

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			totalN += int64(n)
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				break
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

	return resp.StatusCode, bytesOut
}

func HandleForward(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var (
			status      int = 200
			bytesOut    int64
			upstreamURL string
		)
		defer func() {
			s.Logger.Info("response done",
				"method", r.Method,
				"path", r.URL.Path,
				"upstream_url", upstreamURL,
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

		s.Logger.Info("request received",
			"method", r.Method,
			"path", r.URL.Path,
		)

		s.Logger.Trace("client request",
			"method", r.Method, "path", r.URL.Path,
			"headers", logging.RedactHeaders(r.Header),
			"body_size", len(body),
			"body", bodyAttr(s.Logger, body),
		)

		upstream := s.Config.DefaultUpstream()
		if upstream == nil {
			status = http.StatusInternalServerError
			sendProxyError(w, status, "default upstream is not configured")
			return
		}
		client := s.clientFor(upstream)
		upstreamPath := stripAPIPrefix(r.URL.RequestURI())

		ct := r.Header.Get("Content-Type")
		if strings.Contains(ct, "application/json") && len(body) > 0 {
			var data map[string]interface{}
			if err := json.Unmarshal(body, &data); err == nil {
				if model, ok := data["model"].(string); ok && model != "" {
					route, err := s.Config.RouteModel(model)
					if err != nil {
						status = http.StatusInternalServerError
						sendProxyError(w, status, err.Error())
						return
					}
					upstream = route.Upstream
					client = s.clientFor(upstream)
					if route.UpstreamModel != model {
						s.Logger.Debug("model rewritten", "from", model, "to", route.UpstreamModel)
						data["model"] = route.UpstreamModel
						body, _ = json.Marshal(data)
						s.Logger.Trace("proxy rewritten body",
							"body_size", len(body),
							"body", bodyAttr(s.Logger, body),
						)
					}
				}
			}
		}

		upstreamURL = upstream.ResolveURL(upstreamPath)
		status, bytesOut = doForwardRequest(s.Logger, client, w, r, upstreamURL, body)
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
