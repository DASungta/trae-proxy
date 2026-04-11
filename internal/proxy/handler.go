package proxy

import (
	"bufio"
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

const traceCap = 1 << 20 // 1MiB cap for streaming trace buffers

// bodyAttr returns the body value for a log attribute.
// When logger.LogBody() is true the full content is returned;
// otherwise a 512-byte snippet is returned.
func bodyAttr(logger *logging.Logger, b []byte) string {
	if logger.LogBody() {
		return string(b)
	}
	return logging.Snippet(b, 512)
}

func HandleChatCompletions(cfg *config.Config, logger *logging.Logger, client *http.Client) http.HandlerFunc {
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

		var reqData map[string]interface{}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &reqData); err != nil {
				reqData = map[string]interface{}{}
			}
		} else {
			reqData = map[string]interface{}{}
		}

		origModel, _ := reqData["model"].(string)
		isStream, _ := reqData["stream"].(bool)

		anthropicReq := ChatToAnthropic(reqData, cfg.MapModel)

		// Tap 2: proxy internal form (debug shows parsed fields, trace shows full maps).
		logger.Debug("parsed request",
			"model", origModel,
			"mapped", cfg.MapModel(origModel),
			"stream", isStream,
		)
		if logger.Enabled(logging.LevelTrace) {
			anthropicJSON, _ := json.Marshal(anthropicReq)
			logger.Trace("proxy internal form",
				"openai_body", bodyAttr(logger, body),
				"anthropic_body", bodyAttr(logger, anthropicJSON),
			)
		}

		upstreamBody, _ := json.Marshal(anthropicReq)

		url := cfg.Upstream + "/v1/messages"
		req, err := http.NewRequestWithContext(r.Context(), "POST", url, strings.NewReader(string(upstreamBody)))
		if err != nil {
			status = http.StatusBadGateway
			sendProxyError(w, http.StatusBadGateway, fmt.Sprintf("failed to create request: %v", err))
			return
		}

		req.Header.Set("Content-Type", "application/json")
		av := r.Header.Get("anthropic-version")
		if av == "" {
			av = "2023-06-01"
		}
		req.Header.Set("anthropic-version", av)
		for _, h := range forwardHeaders {
			if h == "Content-Type" || h == "anthropic-version" {
				continue
			}
			if v := r.Header.Get(h); v != "" {
				req.Header.Set(h, v)
			}
		}

		// Tap 3: upstream request payload.
		logger.Trace("upstream request",
			"url", url,
			"headers", logging.RedactHeaders(req.Header),
			"body_size", len(upstreamBody),
			"body", bodyAttr(logger, upstreamBody),
		)

		resp, err := client.Do(req)
		if err != nil {
			status = http.StatusBadGateway
			sendProxyError(w, http.StatusBadGateway, fmt.Sprintf("upstream unreachable: %s", cfg.Upstream))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			status = resp.StatusCode
			respBody, _ := io.ReadAll(resp.Body)
			// Always log upstream errors (even below trace) to ease debugging.
			logger.Error("upstream error",
				"status", resp.StatusCode,
				"body", logging.Snippet(respBody, 4096),
			)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(resp.StatusCode)
			w.Write(respBody)
			return
		}

		if isStream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(200)

			flusher, canFlush := w.(http.Flusher)
			conv := NewStreamConverter(origModel)

			scanner := bufio.NewScanner(resp.Body)
			scanner.Buffer(make([]byte, 64*1024), 64*1024)

			var dataBuffer strings.Builder
			var writeErr error

			// Trace buffers for streaming tap 4.
			var upstreamTrace, clientTrace bytes.Buffer

			writeAndFlush := func(data []byte) {
				if writeErr != nil {
					return
				}
				n, werr := w.Write(data)
				bytesOut += int64(n)
				writeErr = werr
				if writeErr == nil && canFlush {
					flusher.Flush()
				}
			}

			for scanner.Scan() && writeErr == nil {
				line := scanner.Text()

				if strings.HasPrefix(line, "data:") {
					if dataBuffer.Len() > 0 {
						raw := dataBuffer.String()
						if logger.Enabled(logging.LevelTrace) {
							logging.AppendCapped(&upstreamTrace, []byte(raw+"\n"), traceCap)
						}
						out := conv.Feed(raw)
						if out != "" {
							if logger.Enabled(logging.LevelTrace) {
								logging.AppendCapped(&clientTrace, []byte(out), traceCap)
							}
							writeAndFlush([]byte(out))
						}
						dataBuffer.Reset()
					}
					dataBuffer.WriteString(line)
				} else if dataBuffer.Len() > 0 && strings.TrimSpace(line) != "" {
					dataBuffer.WriteString(line)
				} else if dataBuffer.Len() > 0 && strings.TrimSpace(line) == "" {
					raw := dataBuffer.String()
					if logger.Enabled(logging.LevelTrace) {
						logging.AppendCapped(&upstreamTrace, []byte(raw+"\n"), traceCap)
					}
					out := conv.Feed(raw)
					if out != "" {
						if logger.Enabled(logging.LevelTrace) {
							logging.AppendCapped(&clientTrace, []byte(out), traceCap)
						}
						writeAndFlush([]byte(out))
					}
					dataBuffer.Reset()
				}
			}
			if dataBuffer.Len() > 0 && writeErr == nil {
				raw := dataBuffer.String()
				if logger.Enabled(logging.LevelTrace) {
					logging.AppendCapped(&upstreamTrace, []byte(raw+"\n"), traceCap)
				}
				out := conv.Feed(raw)
				if out != "" {
					if logger.Enabled(logging.LevelTrace) {
						logging.AppendCapped(&clientTrace, []byte(out), traceCap)
					}
					writeAndFlush([]byte(out))
				}
			}

			// Tap 4 (stream): log accumulated upstream SSE + converted client output.
			if logger.Enabled(logging.LevelTrace) {
				logger.Trace("upstream stream response",
					"status", resp.StatusCode,
					"body_size", upstreamTrace.Len(),
					"body", bodyAttr(logger, upstreamTrace.Bytes()),
				)
				logger.Trace("client stream response",
					"body_size", clientTrace.Len(),
					"body", bodyAttr(logger, clientTrace.Bytes()),
				)
			}
		} else {
			respBody, _ := io.ReadAll(resp.Body)
			var respData map[string]interface{}
			if err := json.Unmarshal(respBody, &respData); err != nil {
				// Tap 4 (non-stream, parse error): pass through raw body.
				logger.Trace("upstream response (raw)",
					"status", resp.StatusCode,
					"body_size", len(respBody),
					"body", bodyAttr(logger, respBody),
				)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(200)
				n, _ := w.Write(respBody)
				bytesOut = int64(n)
				return
			}

			// Tap 4 (non-stream): upstream + converted response.
			logger.Trace("upstream response",
				"status", resp.StatusCode,
				"body_size", len(respBody),
				"body", bodyAttr(logger, respBody),
			)

			chatResp := AnthropicToChat(respData, origModel)
			logger.Trace("client response",
				"body", chatResp,
			)

			w.Header().Set("Content-Type", "application/json")
			enc := json.NewEncoder(w)
			if err := enc.Encode(chatResp); err == nil {
				// Approximate bytes_out: encode to buffer to count.
				var buf bytes.Buffer
				json.NewEncoder(&buf).Encode(chatResp)
				bytesOut = int64(buf.Len())
			}
		}
	}
}
