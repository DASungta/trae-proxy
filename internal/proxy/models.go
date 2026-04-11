package proxy

import (
	"encoding/json"
	"fmt"
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
			fmt.Println("[models] real models fetch failed, falling back to config list")
		}
		serveFakeModels(s, w)
	}
}

// forwardToRealModels proxies GET /v1/models to the real hijack domain via
// BypassClient (which resolves DNS via 1.1.1.1, ignoring /etc/hosts).
// Returns true on success.
func forwardToRealModels(s *Server, w http.ResponseWriter, r *http.Request) bool {
	url := fmt.Sprintf("https://%s/api/v1/models", s.Config.Hijack)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Printf("[models] build request error: %v\n", err)
		return false
	}
	// Forward Accept header; intentionally omit Authorization (public endpoint).
	if v := r.Header.Get("Accept"); v != "" {
		req.Header.Set("Accept", v)
	}

	resp, err := s.BypassClient.Do(req)
	if err != nil {
		fmt.Printf("[models] upstream error: %v\n", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("[models] upstream returned %d\n", resp.StatusCode)
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
