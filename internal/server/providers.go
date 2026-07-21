package server

import (
	"encoding/json"
	"net/http"

	"github.com/j27-aurum/gofast/internal/provider"
)

// ProvidersHandler serves GET /api/providers — known providers with effective settings.
func ProvidersHandler(reg *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(reg.Providers())
	}
}
