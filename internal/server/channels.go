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
