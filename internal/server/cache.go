package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/channelattr"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/providerset"
	"github.com/j27-aurum/gofast/internal/refresh"
)

// CacheInventoryResponse is GET /api/cache.
type CacheInventoryResponse struct {
	cache.Inventory
	ChannelAttr channelattr.Stats `json:"channelattr"`
}

// CacheInventoryHandler serves GET /api/cache.
func CacheInventoryHandler(cc *cache.Cache, attrs *channelattr.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		inv, err := cc.Inventory(providerset.Known())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var attrStats channelattr.Stats
		if attrs != nil {
			attrStats, err = attrs.Stats(r.Context())
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		writeJSON(w, http.StatusOK, CacheInventoryResponse{Inventory: inv, ChannelAttr: attrStats})
	}
}

// CachePurgeAllHandler serves POST /api/cache/purge.
func CachePurgeAllHandler(svc *refresh.Service, runCtx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !sameOrigin(r) {
			http.Error(w, "cross-origin request rejected", http.StatusForbidden)
			return
		}
		result, err := svc.PurgeAllAndRefresh(runCtx, logosQuery(r))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		status := http.StatusAccepted
		if result.Refresh == "skipped" {
			status = http.StatusOK
		}
		writeJSON(w, status, result)
	}
}

// ProviderCachePurgeHandler serves POST /api/providers/{id}/cache/purge.
func ProviderCachePurgeHandler(svc *refresh.Service, runCtx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !sameOrigin(r) {
			http.Error(w, "cross-origin request rejected", http.StatusForbidden)
			return
		}
		id := model.ProviderID(r.PathValue("id"))
		result, err := svc.PurgeAndRefresh(runCtx, id, logosQuery(r))
		if errors.Is(err, refresh.ErrUnknownProvider) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, refresh.ErrRefreshInFlight) {
			writeJSON(w, http.StatusConflict, result)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusAccepted, result)
	}
}

// LogosClearHandler serves DELETE /api/logos, /api/logos/{provider}, and
// /api/logos/{provider}/{channelId}.
func LogosClearHandler(svc *refresh.Service, runCtx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !sameOrigin(r) {
			http.Error(w, "cross-origin request rejected", http.StatusForbidden)
			return
		}
		providerID := model.ProviderID(r.PathValue("provider"))
		channelID := r.PathValue("channelId")

		var (
			stats cache.ClearStats
			err   error
		)
		switch {
		case providerID == "" && channelID == "":
			stats, err = svc.ClearAllLogos(runCtx)
		case channelID == "":
			stats, err = svc.ClearProviderLogos(runCtx, providerID)
		default:
			stats, err = svc.ClearChannelLogo(runCtx, providerID, channelID)
		}
		if errors.Is(err, refresh.ErrUnknownProvider) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, stats)
	}
}

func logosQuery(r *http.Request) bool {
	v := strings.TrimSpace(r.URL.Query().Get("logos"))
	if v == "" {
		return false
	}
	b, err := strconv.ParseBool(v)
	if err == nil {
		return b
	}
	return v == "1"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
