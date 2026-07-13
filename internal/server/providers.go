package server

import (
	"encoding/json"
	"net/http"

	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/model"
)

// ProvidersHandler serves GET /api/providers — the configured provider list only.
func ProvidersHandler(cfg *config.Config) http.HandlerFunc {
	list := model.ListProviders(cfg.Providers)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	}
}
