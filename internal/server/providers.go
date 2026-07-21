package server

import (
	"encoding/json"
	"net/http"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

type providerDetail struct {
	Settings model.ProviderSettings `json:"settings"`
	Stats    provider.Stats         `json:"stats"`
}

// ProviderDetailHandler serves GET /api/providers/{id}.
func ProviderDetailHandler(reg *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		settings, ok := reg.Provider(model.ProviderID(r.PathValue("id")))
		if !ok {
			http.NotFound(w, r)
			return
		}
		detail := providerDetail{Settings: settings}
		if feed, ok := reg.Feed(settings.ID); ok {
			detail.Stats = feed.Stats()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(detail)
	}
}

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
