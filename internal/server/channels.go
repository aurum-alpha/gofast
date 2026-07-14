package server

import (
	"encoding/json"
	"net/http"

	"github.com/j27-aurum/gofast/internal/snapshot"
)

// ChannelsHandler serves GET /api/channels from last-good refresh snapshots.
func ChannelsHandler(store *snapshot.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(store.ListChannels())
	}
}
