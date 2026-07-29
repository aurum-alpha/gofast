package server

import (
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"strings"

	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/dedupe"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

type dedupesResponse struct {
	Revision           string                `json:"revision"`
	ReadOnly           bool                  `json:"read_only"`
	PreferredProviders []model.ProviderID    `json:"preferred_providers"`
	KeepAllKeys        []string              `json:"keep_all_keys"`
	Summary            dedupe.Summary        `json:"summary"`
	Clusters           []dedupe.Cluster      `json:"clusters"`
	Reloads            []config.ReloadResult `json:"reloads,omitempty"`
}

type dedupeApplyRequest struct {
	Revision           string              `json:"revision"`
	PreferredProviders []model.ProviderID  `json:"preferred_providers"`
	KeepAllKeys        []string            `json:"keep_all_keys"`
	Actions            []dedupeApplyAction `json:"actions"`
}

type dedupeApplyAction struct {
	Provider model.ProviderID `json:"provider"`
	ID       string           `json:"id"` // normalized_id
	Export   model.ExportMode `json:"export"`
}

// DedupesHandler serves GET /api/dedupes — live title clusters + preferences.
func DedupesHandler(store *config.Store, reg *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeDedupes(w, store, reg, nil)
	}
}

// DedupesApplyHandler serves PUT /api/dedupes/apply — channel_emit export
// updates plus optional dedupe preference lists.
func DedupesApplyHandler(store *config.Store, reg *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !sameOrigin(r) {
			http.Error(w, "cross-origin request rejected", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
		var req dedupeApplyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		cfg := store.Current()
		if cfg == nil {
			http.Error(w, "config not loaded", http.StatusInternalServerError)
			return
		}

		ops, err := buildDedupeApplyOps(cfg, req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		if len(ops) == 0 {
			writeDedupes(w, store, reg, nil)
			return
		}
		reloads, err := store.Save(r.Context(), req.Revision, ops)
		if err != nil {
			switch {
			case errors.Is(err, config.ErrStaleRevision):
				http.Error(w, "config changed since it was loaded; reload and retry", http.StatusConflict)
			case errors.Is(err, config.ErrReadOnly):
				http.Error(w, "config file is read-only; mount /data (or the config path) read-write to save dedupe settings", http.StatusForbidden)
			case errors.Is(err, config.ErrInvalid):
				http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			default:
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		writeDedupes(w, store, reg, reloads)
	}
}

func buildDedupeApplyOps(cfg *config.Config, req dedupeApplyRequest) ([]config.PathOp, error) {
	var ops []config.PathOp

	// Always persist the preference lists from the request (UI sends the full draft).
	doc := dedupe.Doc{
		PreferredProviders: append([]model.ProviderID(nil), req.PreferredProviders...),
		KeepAllKeys:        append([]string(nil), req.KeepAllKeys...),
	}.Normalized()
	if doc.IsZero() {
		ops = append(ops, config.PathOp{Path: "dedupe", Remove: true})
	} else {
		ops = append(ops, config.PathOp{Path: "dedupe", Value: doc})
	}

	// Group emit updates by provider.
	type change struct {
		id     string
		export model.ExportMode
	}
	byProv := map[model.ProviderID][]change{}
	for _, a := range req.Actions {
		id := strings.TrimSpace(a.ID)
		if a.Provider == "" || id == "" {
			return nil, errors.New("actions: provider and id are required")
		}
		mode := model.ExportMode(strings.ToLower(strings.TrimSpace(string(a.Export))))
		switch mode {
		case model.ExportAuto, model.ExportEnabled, model.ExportDisabled, "":
			if mode == "" {
				mode = model.ExportAuto
			}
		default:
			return nil, errors.New("actions: invalid export mode")
		}
		byProv[a.Provider] = append(byProv[a.Provider], change{id: id, export: mode})
	}

	for providerID, changes := range byProv {
		overlay := cfg.Providers[providerID]
		emitMap := maps.Clone(overlay.ChannelEmit)
		if emitMap == nil {
			emitMap = map[string]model.ChannelEmit{}
		}
		for _, ch := range changes {
			row := emitMap[ch.id]
			switch ch.export {
			case model.ExportEnabled:
				row.Export = model.ExportEnabled
				row.Dedupe = false
			case model.ExportDisabled:
				row.Export = model.ExportDisabled
				row.Dedupe = true
			default:
				row.Export = ""
				row.Dedupe = false
			}
			norm := row.Normalized()
			if err := norm.Validate(); err != nil {
				return nil, err
			}
			if norm.IsZero() {
				delete(emitMap, ch.id)
			} else {
				emitMap[ch.id] = norm
			}
		}
		path := "providers." + string(providerID) + ".channel_emit"
		if len(emitMap) == 0 {
			ops = append(ops, config.PathOp{Path: path, Remove: true})
		} else {
			ops = append(ops, config.PathOp{Path: path, Value: emitMap})
		}
	}
	return ops, nil
}

func writeDedupes(w http.ResponseWriter, store *config.Store, reg *provider.Registry, reloads []config.ReloadResult) {
	cfg := store.Current()
	doc := dedupe.Doc{}
	if cfg != nil {
		doc = cfg.Dedupe.Normalized()
	}
	keepAll := map[string]bool{}
	for _, k := range doc.KeepAllKeys {
		keepAll[k] = true
	}
	labels := map[model.ProviderID]string{}
	channels := make([]model.Channel, 0)
	if reg != nil {
		for _, f := range reg.Feeds() {
			labels[f.ID()] = f.Label()
			emitMap := map[string]model.ChannelEmit{}
			if cfg != nil {
				if s, ok := cfg.Providers[f.ID()]; ok {
					emitMap = s.ChannelEmit
					if s.Label != "" {
						labels[f.ID()] = s.Label
					}
				}
			}
			for _, ch := range f.Channels() {
				painted := model.PaintChannelEmit([]model.Channel{ch}, emitMap)
				ch = painted[0]
				// Reflect emit:disabled even if prepare has not re-run yet.
				if ch.Emit != nil && ch.Emit.ExportMode() == model.ExportDisabled {
					if ch.Emit.Dedupe {
						ch.AddFilterReason(model.FilterReasonDuplicate)
					} else {
						ch.AddFilterReason(model.FilterReasonEmitDisabled)
					}
				}
				if ch.Emit != nil && ch.Emit.ExportMode() == model.ExportEnabled {
					ch.ClearSoftFilterReasons()
				}
				channels = append(channels, ch)
			}
		}
	}
	clusters := dedupe.Scan(channels, labels, keepAll)
	summary := dedupe.Summarize(clusters, keepAll)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(dedupesResponse{
		Revision:           store.Revision(),
		ReadOnly:           configReadOnly(store.Path()),
		PreferredProviders: doc.PreferredProviders,
		KeepAllKeys:        doc.KeepAllKeys,
		Summary:            summary,
		Clusters:           clusters,
		Reloads:            reloads,
	})
}
