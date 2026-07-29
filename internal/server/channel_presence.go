package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/j27-aurum/gofast/internal/channelattr"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

type presenceHistoryResponse struct {
	Events []channelattr.HistoryEvent `json:"events"`
}

// ChannelPresenceHistoryHandler serves GET .../presence/history.
// Works for live and absent (ghost) channels when presence history exists.
func ChannelPresenceHistoryHandler(reg *provider.Registry, attrs *channelattr.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if attrs == nil {
			http.Error(w, "channel attributes unavailable", http.StatusServiceUnavailable)
			return
		}
		providerID := model.ProviderID(r.PathValue("provider"))
		normalizedID := r.PathValue("normalizedId")
		if _, ok := findChannelOrGhost(reg, attrs, string(providerID), normalizedID); !ok {
			http.NotFound(w, r)
			return
		}
		limit := 0
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 0 {
				http.Error(w, "invalid limit", http.StatusBadRequest)
				return
			}
			limit = n
		}
		events, err := attrs.History(r.Context(), providerID, normalizedID, channelattr.KindPresence, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if events == nil {
			events = []channelattr.HistoryEvent{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(presenceHistoryResponse{Events: events})
	}
}

// PresenceSummaryHandler serves GET /api/presence/summary for Status rollups.
func PresenceSummaryHandler(attrs *channelattr.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if attrs == nil {
			http.Error(w, "channel attributes unavailable", http.StatusServiceUnavailable)
			return
		}
		sum, err := attrs.SummarizePresence(r.Context(), time.Now().UTC())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sum)
	}
}
