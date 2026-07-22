package server

import (
	"encoding/json"
	"net/http"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

// channelList is the GET /api/channels JSON envelope.
type channelList struct {
	Channels []model.Channel `json:"channels"`
}

// ChannelsHandler serves GET /api/channels from the registry's live feeds.
func ChannelsHandler(reg *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(channelList{Channels: reg.Channels()})
	}
}

// ChannelHandler serves GET /api/channels/{provider}/{normalizedId}.
func ChannelHandler(reg *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		providerID := model.ProviderID(r.PathValue("provider"))
		normalizedID := r.PathValue("normalizedId")
		feed, ok := reg.Feed(providerID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		for _, ch := range feed.Channels() {
			if ch.NormalizedID == normalizedID {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(ch)
				return
			}
		}
		http.NotFound(w, r)
	}
}
