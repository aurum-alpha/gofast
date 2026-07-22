package server

import (
	"encoding/json"
	"net/http"

	"github.com/j27-aurum/gofast/internal/refresh"
)

// StatusHandler serves GET /api/status (boot / logo-cache progress).
func StatusHandler(st *refresh.Status) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(st.Snapshot())
	}
}
