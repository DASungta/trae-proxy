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

func HandleChatCompletions(s *Server) http.HandlerFunc {
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

		s.Logger.Trace("client request",
			"method", r.Method, "path", r.URL.Path,
			"headers", logging.RedactHeaders(r.Header),
			"body_size", len(body),
			"body", bodyAttr(s.Logger, body),
		)

		reqData := map[string]interface{}{}
		parsedJSON := false
		if len(body) > 0 {
			if err := json.Unmarshal(body, &reqData); err == nil {
				parsedJSON = true
			}
		} else {
			parsedJSON = true
		}

		origModel, _ := reqData["model"].(string)
		isStream, _ := reqData["stream"].(bool)

		route, err := s.Config.RouteModel(origModel)
		if err != nil {
			status = http.StatusInternalServerError
			sendProxyError(w, status, err.Error())
			return
		}

		s.Logger.Info("request received",
			"method", r.Method,
			"path", r.URL.Path,
			"model", origModel,
			"mapped", route.UpstreamModel,
			"protocol", route.Upstream.Protocol,
			"stream", isStream,
		)

		s.Logger.Debug("parsed request",
			"model", origModel,
			"mapped", route.UpstreamModel,
			"protocol", route.Upstream.Protocol,
			"stream", isStream,
		)

		switch route.Upstream.Protocol {
		case "anthropic":
			anthropicReq := ChatToAnthropic(reqData, func(string) string { return route.UpstreamModel })
			if s.Logger.Enabled(logging.LevelTrace) {
				anthropicJSON, _ := json.Marshal(anthropicReq)
				s.Logger.Trace("proxy internal form",
					"openai_body", bodyAttr(s.Logger, body),
					"anthropic_body", bodyAttr(s.Logger, anthropicJSON),
				)
			}

			upstreamBody, _ := json.Marshal(anthropicReq)
			upstreamURL = route.Upstream.ResolveURL("/v1/messages")
			req, err := http.NewRequestWithContext(r.Context(), "POST", upstreamURL, strings.NewReader(string(upstreamBody)))
			if err != nil {
				status = http.StatusBadGateway
				sendProxyError(w, status, fmt.Sprintf("failed to create request: %v", err))
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

			s.Logger.Trace("upstream request",
				"url", upstreamURL,
				"headers", logging.RedactHeaders(req.Header),
				"body_size", len(upstreamBody),
				"body", bodyAttr(s.Logger, upstreamBody),
			)

			resp, err := s.clientFor(route.Upstream).Do(req)
			if err != nil {
				status = http.StatusBadGateway
				sendProxyError(w, status, fmt.Sprintf("upstream unreachable: %s", route.Upstream.URL))
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode >= 400 {
				status = resp.StatusCode
				respBody, _ := io.ReadAll(resp.Body)
				s.Logger.Error("upstream error",
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
							if s.Logger.Enabled(logging.LevelTrace) {
								logging.AppendCapped(&upstreamTrace, []byte(raw+"\n"), traceCap)
							}
							out := conv.Feed(raw)
							if out != "" {
								if s.Logger.Enabled(logging.LevelTrace) {
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
						if s.Logger.Enabled(logging.LevelTrace) {
							logging.AppendCapped(&upstreamTrace, []byte(raw+"\n"), traceCap)
						}
						out := conv.Feed(raw)
						if out != "" {
							if s.Logger.Enabled(logging.LevelTrace) {
								logging.AppendCapped(&clientTrace, []byte(out), traceCap)
							}
							writeAndFlush([]byte(out))
						}
						dataBuffer.Reset()
					}
				}
				if dataBuffer.Len() > 0 && writeErr == nil {
					raw := dataBuffer.String()
					if s.Logger.Enabled(logging.LevelTrace) {
						logging.AppendCapped(&upstreamTrace, []byte(raw+"\n"), traceCap)
					}
					out := conv.Feed(raw)
					if out != "" {
						if s.Logger.Enabled(logging.LevelTrace) {
							logging.AppendCapped(&clientTrace, []byte(out), traceCap)
						}
						writeAndFlush([]byte(out))
					}
				}

				if s.Logger.Enabled(logging.LevelTrace) {
					s.Logger.Trace("upstream stream response",
						"status", resp.StatusCode,
						"body_size", upstreamTrace.Len(),
						"body", bodyAttr(s.Logger, upstreamTrace.Bytes()),
					)
					s.Logger.Trace("client stream response",
						"body_size", clientTrace.Len(),
						"body", bodyAttr(s.Logger, clientTrace.Bytes()),
					)
				}
				return
			}

			respBody, _ := io.ReadAll(resp.Body)
			var respData map[string]interface{}
			if err := json.Unmarshal(respBody, &respData); err != nil {
				s.Logger.Trace("upstream response (raw)",
					"status", resp.StatusCode,
					"body_size", len(respBody),
					"body", bodyAttr(s.Logger, respBody),
				)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(200)
				n, _ := w.Write(respBody)
				bytesOut = int64(n)
				return
			}

			s.Logger.Trace("upstream response",
				"status", resp.StatusCode,
				"body_size", len(respBody),
				"body", bodyAttr(s.Logger, respBody),
			)

			chatResp := AnthropicToChat(respData, origModel)
			s.Logger.Trace("client response", "body", chatResp)

			w.Header().Set("Content-Type", "application/json")
			enc := json.NewEncoder(w)
			if err := enc.Encode(chatResp); err == nil {
				var buf bytes.Buffer
				json.NewEncoder(&buf).Encode(chatResp)
				bytesOut = int64(buf.Len())
			}
			return

		case "openai":
			if parsedJSON {
				reqData["model"] = route.UpstreamModel
				body, _ = json.Marshal(reqData)
				s.Logger.Trace("proxy rewritten body",
					"body_size", len(body),
					"body", bodyAttr(s.Logger, body),
				)
			}
			upstreamURL = route.Upstream.ResolveURL("/v1/chat/completions")
			status, bytesOut = doForwardRequest(s.Logger, s.clientFor(route.Upstream), w, r, upstreamURL, body)
			return
		default:
			status = http.StatusInternalServerError
			sendProxyError(w, status, "unsupported protocol: "+route.Upstream.Protocol)
			return
		}
	}
}
