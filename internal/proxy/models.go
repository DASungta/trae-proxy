package proxy

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/zhangyc/trae-proxy/internal/config"
)

func HandleModels(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ids := cfg.ModelIDs()
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
}
