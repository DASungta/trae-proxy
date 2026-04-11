package proxy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/zhangyc/trae-proxy/internal/config"
)

func HandleChatCompletions(cfg *config.Config, client *http.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

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
		upstreamBody, _ := json.Marshal(anthropicReq)

		url := cfg.Upstream + "/v1/messages"
		req, err := http.NewRequestWithContext(r.Context(), "POST", url, strings.NewReader(string(upstreamBody)))
		if err != nil {
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

		resp, err := client.Do(req)
		if err != nil {
			sendProxyError(w, http.StatusBadGateway, fmt.Sprintf("upstream unreachable: %s", cfg.Upstream))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			respBody, _ := io.ReadAll(resp.Body)
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
			writeAndFlush := func(data []byte) {
				if writeErr != nil {
					return
				}
				_, writeErr = w.Write(data)
				if writeErr == nil && canFlush {
					flusher.Flush()
				}
			}

			for scanner.Scan() && writeErr == nil {
				line := scanner.Text()

				if strings.HasPrefix(line, "data:") {
					if dataBuffer.Len() > 0 {
						out := conv.Feed(dataBuffer.String())
						if out != "" {
							writeAndFlush([]byte(out))
						}
						dataBuffer.Reset()
					}
					dataBuffer.WriteString(line)
				} else if dataBuffer.Len() > 0 && strings.TrimSpace(line) != "" {
					dataBuffer.WriteString(line)
				} else if dataBuffer.Len() > 0 && strings.TrimSpace(line) == "" {
					out := conv.Feed(dataBuffer.String())
					if out != "" {
						writeAndFlush([]byte(out))
					}
					dataBuffer.Reset()
				}
			}
			if dataBuffer.Len() > 0 && writeErr == nil {
				out := conv.Feed(dataBuffer.String())
				if out != "" {
					writeAndFlush([]byte(out))
				}
			}
		} else {
			respBody, _ := io.ReadAll(resp.Body)
			var respData map[string]interface{}
			if err := json.Unmarshal(respBody, &respData); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(200)
				w.Write(respBody)
				return
			}
			chatResp := AnthropicToChat(respData, origModel)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(chatResp)
		}
	}
}
