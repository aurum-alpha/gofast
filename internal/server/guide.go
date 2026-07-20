package server

import (
	"encoding/json"
	"net/http"

	"github.com/j27-aurum/gofast/internal/snapshot"
)

// GuideHandler serves GET /api/guide — channels with their programmes from the
// last-good refresh snapshots. Filtering is done client-side (like /api/channels).
func GuideHandler(store *snapshot.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(store.ListGuide())
	}
}
