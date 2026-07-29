package server

import (
	"encoding/json"
	"net/http"

	"github.com/j27-aurum/gofast/internal/channelattr"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

// channelList is the GET /api/channels JSON envelope.
type channelList struct {
	Channels []model.Channel `json:"channels"`
}

// ChannelsHandler serves GET /api/channels from the registry's live feeds,
// plus ghost rows for Current presence=absent (dropped from provider catalog).
func ChannelsHandler(reg *provider.Registry, attrs *channelattr.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		chs := reg.Channels()
		chs = appendAbsentGhosts(chs, attrs)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(channelList{Channels: chs})
	}
}

// ChannelHandler serves GET /api/channels/{provider}/{normalizedId}.
// Falls back to an absent ghost when the id is no longer on the live feed.
func ChannelHandler(reg *provider.Registry, attrs *channelattr.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		providerID := model.ProviderID(r.PathValue("provider"))
		normalizedID := r.PathValue("normalizedId")
		ch, ok := findChannelOrGhost(reg, attrs, string(providerID), normalizedID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ch)
	}
}

func appendAbsentGhosts(chs []model.Channel, attrs *channelattr.Store) []model.Channel {
	if attrs == nil {
		return chs
	}
	live := make(map[string]struct{}, len(chs))
	for _, ch := range chs {
		live[string(ch.Provider)+"\x00"+ch.NormalizedID] = struct{}{}
	}
	for _, ent := range attrs.AbsentEntries() {
		key := string(ent.Provider) + "\x00" + ent.ChannelID
		if _, ok := live[key]; ok {
			continue
		}
		chs = append(chs, channelattr.GhostChannel(ent.Provider, ent.ChannelID, ent.Presence))
	}
	return chs
}

func findChannelOrGhost(reg *provider.Registry, attrs *channelattr.Store, providerID, normalizedID string) (model.Channel, bool) {
	if ch, ok := findChannel(reg, providerID, normalizedID); ok {
		return ch, true
	}
	if attrs == nil || providerID == "" || normalizedID == "" {
		return model.Channel{}, false
	}
	if _, ok := reg.Feed(model.ProviderID(providerID)); !ok {
		// Provider unknown/disabled — no UI surface for its ghosts.
		return model.Channel{}, false
	}
	p, ok := attrs.CurrentPresence(model.ProviderID(providerID))[normalizedID]
	if !ok || p.State != channelattr.PresenceAbsent {
		return model.Channel{}, false
	}
	return channelattr.GhostChannel(model.ProviderID(providerID), normalizedID, p), true
}
