package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/j27-aurum/gofast/internal/channelattr"
	"github.com/j27-aurum/gofast/internal/health"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

type healthHistoryResponse struct {
	Events         []channelattr.HistoryEvent `json:"events"`
	SuccessRate30d *float64                   `json:"success_rate_30d"`
}

type healthProbeResponse struct {
	Check  model.HealthCheck   `json:"check"`
	Health model.ChannelHealth `json:"health"`
}

// ChannelHealthHistoryHandler serves GET .../health/history.
func ChannelHealthHistoryHandler(reg *provider.Registry, attrs *channelattr.Store) http.HandlerFunc {
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
		if _, _, ok := lookupChannel(reg, providerID, normalizedID); !ok {
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
		events, err := attrs.History(r.Context(), providerID, normalizedID, channelattr.KindHealth, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out := healthHistoryResponse{Events: events}
		if rate, ok := channelattr.SuccessRate(events, time.Now().UTC().Add(-30*24*time.Hour)); ok {
			out.SuccessRate30d = &rate
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}

// ChannelHealthProbeHandler serves POST .../health/probe (L3 Test now).
func ChannelHealthProbeHandler(reg *provider.Registry, sched *health.Scheduler) http.HandlerFunc {
	return channelHealthProbe(reg, sched, true)
}

// ChannelHealthProbeL2Handler serves POST .../health/probe/l2 (on-demand L2).
func ChannelHealthProbeL2Handler(reg *provider.Registry, sched *health.Scheduler) http.HandlerFunc {
	return channelHealthProbe(reg, sched, false)
}

func channelHealthProbe(reg *provider.Registry, sched *health.Scheduler, l3 bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if sched == nil {
			http.Error(w, "health prober unavailable", http.StatusServiceUnavailable)
			return
		}
		providerID := model.ProviderID(r.PathValue("provider"))
		normalizedID := r.PathValue("normalizedId")
		feed, ch, ok := lookupChannel(reg, providerID, normalizedID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		var (
			check model.HealthCheck
			next  model.ChannelHealth
			err   error
		)
		if l3 {
			check, next, err = sched.ProbeNow(r.Context(), ch)
		} else {
			check, next, err = sched.ProbeL2Now(r.Context(), ch)
		}
		if err != nil {
			if errors.Is(err, health.ErrNoProber) || errors.Is(err, health.ErrNoSegmentProber) {
				http.Error(w, err.Error(), http.StatusServiceUnavailable)
				return
			}
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		feed.UpdateChannelHealth(normalizedID, next)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(healthProbeResponse{Check: check, Health: next})
	}
}

func lookupChannel(reg *provider.Registry, providerID model.ProviderID, normalizedID string) (*provider.Feed, model.Channel, bool) {
	if reg == nil {
		return nil, model.Channel{}, false
	}
	feed, ok := reg.Feed(providerID)
	if !ok {
		return nil, model.Channel{}, false
	}
	for _, ch := range feed.Channels() {
		if ch.NormalizedID == normalizedID {
			return feed, ch, true
		}
	}
	return nil, model.Channel{}, false
}
