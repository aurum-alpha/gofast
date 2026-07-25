package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/j27-aurum/gofast/internal/categories"
	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/provider"
)

type categoriesMerge struct {
	Name    string   `json:"name"`
	Members []string `json:"members"`
}

type discoveredCategoryProvider struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

type discoveredCategory struct {
	Name       string                       `json:"name"`
	Providers  []discoveredCategoryProvider `json:"providers"`
	Total      int                          `json:"total"`
	AutoMerged bool                         `json:"auto_merged"`
	AssignedTo string                       `json:"assigned_to,omitempty"`
}

type categoryPreview struct {
	ProgrammeCount int `json:"programme_count"`
}

type categoriesResponse struct {
	Enabled    bool                       `json:"enabled"`
	Merges     []categoriesMerge          `json:"merges"`
	Discovered []discoveredCategory       `json:"discovered"`
	Preview    map[string]categoryPreview `json:"preview"`
	ReadOnly   bool                       `json:"read_only"`
}

type categoriesRequest struct {
	Enabled bool `json:"enabled"`
	Merges  []struct {
		Name    string   `json:"name"`
		Members []string `json:"members"`
	} `json:"merges"`
}

// CategoriesHandler serves GET /api/categories.
func CategoriesHandler(store *config.Store, policy func() *categories.Policy, reg *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeCategories(w, store, policy(), reg)
	}
}

// CategoriesSaveHandler serves PUT /api/categories.
func CategoriesSaveHandler(store *config.Store, policy func() *categories.Policy, reg *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req categoriesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		doc, err := req.toDoc()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ops := []config.PathOp{{Path: "categories", Value: doc}}
		if doc.IsZero() {
			ops = []config.PathOp{{Path: "categories", Remove: true}}
		}
		if _, err := store.Save(r.Context(), "", ops); err != nil {
			switch {
			case errors.Is(err, config.ErrReadOnly):
				http.Error(w, "config file is read-only; mount /data (or the config path) read-write to save categories", http.StatusConflict)
			case errors.Is(err, config.ErrInvalid):
				http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			default:
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		writeCategories(w, store, policy(), reg)
	}
}

func (req categoriesRequest) toDoc() (categories.Doc, error) {
	doc := categories.Doc{Enabled: req.Enabled}
	seen := make(map[string]struct{}, len(req.Merges))
	for _, m := range req.Merges {
		name := strings.TrimSpace(m.Name)
		if name == "" {
			return categories.Doc{}, fmt.Errorf("merge name must not be empty")
		}
		key := strings.ToLower(name)
		if _, dup := seen[key]; dup {
			return categories.Doc{}, fmt.Errorf("duplicate category name %q", name)
		}
		seen[key] = struct{}{}
		members := make([]string, 0, len(m.Members))
		for _, member := range m.Members {
			if s := strings.TrimSpace(member); s != "" {
				members = append(members, s)
			}
		}
		doc.Merges = append(doc.Merges, categories.Merge{Name: name, Members: members})
	}
	return doc, nil
}

func writeCategories(w http.ResponseWriter, store *config.Store, policy *categories.Policy, reg *provider.Registry) {
	doc := store.Current().Categories
	out := categoriesResponse{
		Enabled:    doc.Enabled,
		Merges:     make([]categoriesMerge, 0, len(doc.Merges)),
		Discovered: discoverCategories(reg, policy),
		Preview:    previewCategories(reg, policy),
		ReadOnly:   configReadOnly(store.Path()),
	}
	for _, m := range doc.Merges {
		out.Merges = append(out.Merges, categoriesMerge{Name: m.Name, Members: append([]string(nil), m.Members...)})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func discoverCategories(reg *provider.Registry, policy *categories.Policy) []discoveredCategory {
	type bucket struct {
		name      string
		providers []discoveredCategoryProvider
		total     int
	}
	buckets := map[string]*bucket{}
	var order []string
	for _, f := range reg.Feeds() {
		label := f.Label()
		if label == "" {
			label = string(f.ID())
		}
		counts := map[string]int{}
		for _, p := range f.Programmes() {
			for _, cat := range p.Categories {
				trimmed := strings.TrimSpace(cat)
				if trimmed == "" {
					continue
				}
				counts[trimmed]++
			}
		}
		names := make([]string, 0, len(counts))
		for name := range counts {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			b := buckets[name]
			if b == nil {
				b = &bucket{name: name}
				buckets[name] = b
				order = append(order, name)
			}
			count := counts[name]
			b.providers = append(b.providers, discoveredCategoryProvider{
				ID:    string(f.ID()),
				Label: label,
				Count: count,
			})
			b.total += count
		}
	}
	sort.Strings(order)
	out := make([]discoveredCategory, 0, len(order))
	for _, name := range order {
		b := buckets[name]
		assigned, _ := policy.AssignedName(name)
		out = append(out, discoveredCategory{
			Name:       b.name,
			Providers:  b.providers,
			Total:      b.total,
			AutoMerged: len(b.providers) > 1,
			AssignedTo: assigned,
		})
	}
	return out
}

func previewCategories(reg *provider.Registry, policy *categories.Policy) map[string]categoryPreview {
	out := map[string]categoryPreview{}
	for _, f := range reg.Feeds() {
		for _, p := range f.Programmes() {
			labels := p.ExportCategories()
			if policy != nil && policy.Enabled() && len(p.EmittedCategories) == 0 {
				// Taxonomy on but not yet reapplied to this feed snapshot:
				// preview from live policy over upstream.
				labels = policy.MapAll(p.Categories)
			}
			for _, label := range labels {
				trimmed := strings.TrimSpace(label)
				if trimmed == "" {
					continue
				}
				entry := out[trimmed]
				entry.ProgrammeCount++
				out[trimmed] = entry
			}
		}
	}
	return out
}
