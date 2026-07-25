package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/format"
	"github.com/j27-aurum/gofast/internal/groups"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

// groupsMerge is one canonical group in the API view (enabled resolved to bool).
type groupsMerge struct {
	Name    string   `json:"name"`
	Members []string `json:"members"`
	Enabled bool     `json:"enabled"`
}

// discoveredProvider is one provider's contribution to a discovered upstream group.
type discoveredProvider struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Group    string `json:"group"`
	Count    int    `json:"count"`
	Disabled bool   `json:"disabled"`
}

// discoveredGroup is one upstream group string seen across providers.
type discoveredGroup struct {
	Name       string               `json:"name"`
	Providers  []discoveredProvider `json:"providers"`
	Total      int                  `json:"total"`
	AutoMerged bool                 `json:"auto_merged"`
	AssignedTo string               `json:"assigned_to,omitempty"`
	Disabled   bool                 `json:"disabled"`
}

// groupPreview is the emitted / disabled channel count for one effective group.
type groupPreview struct {
	EmittedCount  int `json:"emitted_count"`
	DisabledCount int `json:"disabled_count"`
}

// groupsResponse is GET /api/groups: the saved taxonomy plus the discovered
// upstream pool and a live effective-group preview.
type groupsResponse struct {
	Enabled    bool                    `json:"enabled"`
	Merges     []groupsMerge           `json:"merges"`
	Disabled   []string                `json:"disabled"`
	Discovered []discoveredGroup       `json:"discovered"`
	Preview    map[string]groupPreview `json:"preview"`
	ReadOnly   bool                    `json:"read_only"`
}

// groupsRequest is the PUT /api/groups body.
type groupsRequest struct {
	Enabled bool `json:"enabled"`
	Merges  []struct {
		Name    string   `json:"name"`
		Members []string `json:"members"`
		Enabled *bool    `json:"enabled"`
	} `json:"merges"`
	Disabled []string `json:"disabled"`
}

// GroupsHandler serves GET /api/groups. The taxonomy doc comes from the config
// snapshot; policy returns the live compiled policy (refresh.Service caches it).
func GroupsHandler(store *config.Store, policy func() *groups.Policy, reg *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeGroups(w, store, policy(), reg)
	}
}

// GroupsSaveHandler serves PUT /api/groups. The taxonomy is one managed config
// key: the save routes through Store.Save (validate → persist comment-preserving
// → reload snapshot → kick every Reloader), so refresh re-applies and the
// aggregate republishes exactly like any other config edit.
func GroupsSaveHandler(store *config.Store, policy func() *groups.Policy, reg *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req groupsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		doc, err := req.toDoc()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ops := []config.PathOp{{Path: "groups", Value: doc}}
		if doc.IsZero() {
			ops = []config.PathOp{{Path: "groups", Remove: true}}
		}
		if _, err := store.Save(r.Context(), "", ops); err != nil {
			switch {
			case errors.Is(err, config.ErrReadOnly):
				http.Error(w, "config file is read-only; mount /data (or the config path) read-write to save groups", http.StatusConflict)
			case errors.Is(err, config.ErrInvalid):
				http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			default:
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		writeGroups(w, store, policy(), reg)
	}
}

func (req groupsRequest) toDoc() (groups.Doc, error) {
	doc := groups.Doc{Enabled: req.Enabled}
	seen := make(map[string]struct{}, len(req.Merges))
	for _, m := range req.Merges {
		name := strings.TrimSpace(m.Name)
		if name == "" {
			return groups.Doc{}, fmt.Errorf("merge name must not be empty")
		}
		key := strings.ToLower(name)
		if _, dup := seen[key]; dup {
			return groups.Doc{}, fmt.Errorf("duplicate group name %q", name)
		}
		seen[key] = struct{}{}
		members := make([]string, 0, len(m.Members))
		for _, member := range m.Members {
			if s := strings.TrimSpace(member); s != "" {
				members = append(members, s)
			}
		}
		merge := groups.Merge{Name: name, Members: members}
		if m.Enabled != nil {
			v := *m.Enabled
			merge.Enabled = &v
		}
		doc.Merges = append(doc.Merges, merge)
	}
	for _, sel := range req.Disabled {
		if s := strings.TrimSpace(sel); s != "" {
			doc.Disabled = append(doc.Disabled, s)
		}
	}
	return doc, nil
}

func writeGroups(w http.ResponseWriter, store *config.Store, policy *groups.Policy, reg *provider.Registry) {
	doc := store.Current().Groups

	out := groupsResponse{
		Enabled:    doc.Enabled,
		Merges:     make([]groupsMerge, 0, len(doc.Merges)),
		Disabled:   append([]string(nil), doc.Disabled...),
		Discovered: discoverGroups(reg, policy),
		Preview:    previewGroups(reg, policy),
		ReadOnly:   configReadOnly(store.Path()),
	}
	if out.Disabled == nil {
		out.Disabled = []string{}
	}
	for _, m := range doc.Merges {
		out.Merges = append(out.Merges, groupsMerge{
			Name:    m.Name,
			Members: m.Members,
			Enabled: m.Enabled == nil || *m.Enabled,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// discoverGroups buckets upstream group strings (exact, trimmed) across all
// providers so identical strings show as auto-merged.
func discoverGroups(reg *provider.Registry, policy *groups.Policy) []discoveredGroup {
	type bucket struct {
		name      string
		providers []discoveredProvider
		total     int
	}
	buckets := map[string]*bucket{}
	order := []string{}
	for _, f := range reg.Feeds() {
		label := f.Label()
		if label == "" {
			label = string(f.ID())
		}
		stats := f.Stats()
		names := make([]string, 0, len(stats.ByGroup))
		for name := range stats.ByGroup {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			trimmed := strings.TrimSpace(name)
			if trimmed == "" || trimmed == "(none)" {
				continue
			}
			b := buckets[trimmed]
			if b == nil {
				b = &bucket{name: trimmed}
				buckets[trimmed] = b
				order = append(order, trimmed)
			}
			count := stats.ByGroup[name]
			_, _, disabled := policy.Lookup(f.ID(), trimmed)
			b.providers = append(b.providers, discoveredProvider{
				ID:       string(f.ID()),
				Label:    label,
				Group:    trimmed,
				Count:    count,
				Disabled: disabled,
			})
			b.total += count
		}
	}
	sort.Strings(order)
	out := make([]discoveredGroup, 0, len(order))
	for _, name := range order {
		b := buckets[name]
		assigned, _ := policy.AssignedName(name)
		_, _, disabled := policy.Lookup("", name)
		out = append(out, discoveredGroup{
			Name:       b.name,
			Providers:  b.providers,
			Total:      b.total,
			AutoMerged: len(b.providers) > 1,
			AssignedTo: assigned,
			Disabled:   disabled,
		})
	}
	return out
}

// previewGroups counts channels per final emitted group-title (same bucket M3U
// uses: Channel.EmittedGroup after taxonomy + per-channel emit, else legacy
// "{label}: {group}"). Disabled-group channels leave EmittedGroup empty, so
// those fall back to the policy lookup for the preview key.
func previewGroups(reg *provider.Registry, policy *groups.Policy) map[string]groupPreview {
	out := map[string]groupPreview{}
	for _, f := range reg.Feeds() {
		label := f.Label()
		if label == "" {
			label = string(f.ID())
		}
		for _, ch := range f.Channels() {
			key, disabled := previewGroupKey(ch, label, f.ID(), policy)
			entry := out[key]
			switch {
			case disabled:
				entry.DisabledCount++
			case ch.Excluded:
				// excluded for another reason (DRM, proxy, unhealthy) — not counted
			default:
				entry.EmittedCount++
			}
			out[key] = entry
		}
	}
	return out
}

func previewGroupKey(ch model.Channel, label string, providerID model.ProviderID, policy *groups.Policy) (key string, disabled bool) {
	if title := strings.TrimSpace(ch.EmittedGroup); title != "" {
		return title, false
	}
	if policy != nil && policy.Enabled() {
		name, mapped, d := policy.Lookup(providerID, ch.Group)
		if mapped || d {
			if name == "" {
				name = strings.TrimSpace(ch.Group)
			}
			return name, d
		}
		return strings.TrimSpace(ch.Group), false
	}
	return format.FormatGroupTitle(label, ch.Group), false
}

// configReadOnly reports whether the config path cannot be written (best-effort
// probe so the UI can warn before a save attempt).
func configReadOnly(path string) bool {
	if path == "" {
		return true
	}
	return errors.Is(config.ProbeWritable(path), config.ErrReadOnly)
}
