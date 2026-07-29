package server

import (
	"encoding/json"
	"errors"
	"maps"
	"net/http"

	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

type channelEmitResponse struct {
	Revision string                `json:"revision"`
	Writable bool                  `json:"writable"`
	Channel  model.Channel         `json:"channel"`
	Reloads  []config.ReloadResult `json:"reloads,omitempty"`
}

type channelEmitRequest struct {
	Revision string             `json:"revision"`
	Emit     *model.ChannelEmit `json:"emit"`
}

// ChannelEmitHandler serves GET /api/channels/{provider}/{normalizedId}/emit —
// the channel plus config revision/writability for the emit editor.
func ChannelEmitHandler(store *config.Store, reg *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ch, ok := findChannel(reg, r.PathValue("provider"), r.PathValue("normalizedId"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		ch = paintEmitFromSettings(ch, reg)
		writeChannelEmit(w, store, ch, nil)
	}
}

// ChannelEmitSaveHandler serves PUT /api/channels/{provider}/{normalizedId}/emit.
// Merges one channel_emit row (or removes it when emit is null/zero) through
// Store.Save as a whole-map PathOp so dotted normalized ids stay valid keys.
func ChannelEmitSaveHandler(store *config.Store, reg *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !sameOrigin(r) {
			http.Error(w, "cross-origin request rejected", http.StatusForbidden)
			return
		}
		providerID := model.ProviderID(r.PathValue("provider"))
		normalizedID := r.PathValue("normalizedId")
		if normalizedID == "" {
			http.Error(w, "missing channel id", http.StatusBadRequest)
			return
		}
		ch, ok := findChannel(reg, string(providerID), normalizedID)
		if !ok {
			http.NotFound(w, r)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var req channelEmitRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}

		cfg := store.Current()
		if cfg == nil {
			http.Error(w, "config not loaded", http.StatusInternalServerError)
			return
		}
		overlay := cfg.Providers[providerID]
		emitMap := maps.Clone(overlay.ChannelEmit)
		if emitMap == nil {
			emitMap = map[string]model.ChannelEmit{}
		}
		if req.Emit == nil || req.Emit.Normalized().IsZero() {
			delete(emitMap, normalizedID)
		} else {
			prev := emitMap[normalizedID]
			row := req.Emit.Normalized()
			// Channel Detail never invents the Dedupes marker; preserve it when
			// export stays disabled so name/logo edits do not flip "duplicate"
			// to "emit disabled".
			if row.ExportMode() == model.ExportDisabled {
				row.Dedupe = prev.Dedupe
			} else {
				row.Dedupe = false
			}
			if err := row.Validate(); err != nil {
				http.Error(w, err.Error(), http.StatusUnprocessableEntity)
				return
			}
			emitMap[normalizedID] = row
		}

		path := "providers." + string(providerID) + ".channel_emit"
		var ops []config.PathOp
		if len(emitMap) == 0 {
			ops = []config.PathOp{{Path: path, Remove: true}}
		} else {
			ops = []config.PathOp{{Path: path, Value: emitMap}}
		}
		reloads, err := store.Save(r.Context(), req.Revision, ops)
		if err != nil {
			switch {
			case errors.Is(err, config.ErrStaleRevision):
				http.Error(w, "config changed since it was loaded; reload and retry", http.StatusConflict)
			case errors.Is(err, config.ErrReadOnly):
				http.Error(w, "config file is read-only; mount /data (or the config path) read-write to save emit settings", http.StatusForbidden)
			case errors.Is(err, config.ErrInvalid):
				http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			default:
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		if updated, ok := findChannel(reg, string(providerID), normalizedID); ok {
			ch = paintEmitFromSettings(updated, reg)
		} else {
			ch = paintEmitFromSettings(ch, reg)
		}
		writeChannelEmit(w, store, ch, reloads)
	}
}

func findChannel(reg *provider.Registry, providerID, normalizedID string) (model.Channel, bool) {
	feed, ok := reg.Feed(model.ProviderID(providerID))
	if !ok {
		return model.Channel{}, false
	}
	for _, ch := range feed.Channels() {
		if ch.NormalizedID == normalizedID {
			return ch, true
		}
	}
	return model.Channel{}, false
}

func paintEmitFromSettings(ch model.Channel, reg *provider.Registry) model.Channel {
	s := reg.Settings(ch.Provider)
	painted := model.PaintChannelEmit([]model.Channel{ch}, s.ChannelEmit)
	return painted[0]
}

func writeChannelEmit(w http.ResponseWriter, store *config.Store, ch model.Channel, reloads []config.ReloadResult) {
	writable := config.ProbeWritable(store.Path()) == nil
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(channelEmitResponse{
		Revision: store.Revision(),
		Writable: writable,
		Channel:  ch,
		Reloads:  reloads,
	})
}
