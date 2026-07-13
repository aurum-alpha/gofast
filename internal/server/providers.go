package server

import (
	"encoding/json"
	"net/http"

	"github.com/j27-aurum/gofast/internal/config"
)

// ProvidersHandler serves GET /api/providers from the loaded config snapshot.
func ProvidersHandler(path string, fromFile bool, cfg config.Config) http.HandlerFunc {
	view := config.ViewProviders(path, fromFile, cfg)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(view)
	}
}
