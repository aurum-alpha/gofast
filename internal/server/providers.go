package server

import (
	"encoding/json"
	"net/http"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

type providerDetail struct {
	Settings model.ProviderSettings `json:"settings"`
	Stats    provider.Stats         `json:"stats"`
}

// providerListRow is one GET /api/providers entry: flat settings fields plus stats.
// ProviderSettings must not be embedded — its MarshalJSON would be promoted and
// drop Stats.
type providerListRow struct {
	Settings model.ProviderSettings
	Stats    provider.Stats
}

func (r providerListRow) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(r.Settings)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	sb, err := json.Marshal(r.Stats)
	if err != nil {
		return nil, err
	}
	m["stats"] = sb
	return json.Marshal(m)
}

type providerListResponse struct {
	Providers []providerListRow `json:"providers"`
}

// ProviderDetailHandler serves GET /api/providers/{id}.
func ProviderDetailHandler(reg *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		settings, ok := reg.Provider(model.ProviderID(r.PathValue("id")))
		if !ok {
			http.NotFound(w, r)
			return
		}
		detail := providerDetail{
			Settings: settings,
			Stats:    provider.EmptyStats(),
		}
		if feed, ok := reg.Feed(settings.ID); ok {
			detail.Stats = feed.Stats()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(detail)
	}
}

// ProvidersHandler serves GET /api/providers — known providers with settings and
// triage stats (full rollups remain on GET /api/providers/{id}).
func ProvidersHandler(reg *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		known := reg.Providers().Providers
		out := make([]providerListRow, 0, len(known))
		for _, settings := range known {
			row := providerListRow{
				Settings: settings,
				Stats:    provider.EmptyStats(),
			}
			if feed, ok := reg.Feed(settings.ID); ok {
				row.Stats = feed.Stats()
			}
			out = append(out, row)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(providerListResponse{Providers: out})
	}
}
