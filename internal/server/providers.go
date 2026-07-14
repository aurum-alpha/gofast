package server

import (
	"encoding/json"
	"net/http"

	"github.com/j27-aurum/gofast/internal/model"
)

// ProvidersHandler serves GET /api/providers — known providers with effective settings.
func ProvidersHandler(settings map[string]model.ProviderSettings) http.HandlerFunc {
	list := model.ListProviders(settings)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	}
}
