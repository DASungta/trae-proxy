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
		models = append(models, map[string]interface{}{
			"id":       id,
			"object":   "model",
			"created":  now,
			"owned_by": "anthropic",
		})
	}
	resp := map[string]interface{}{
		"object": "list",
		"data":   models,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
