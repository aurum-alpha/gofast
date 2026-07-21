package provider

import (
	"log/slog"
	"sort"

	"github.com/j27-aurum/gofast/internal/model"
)

// Registry holds the runtime Feeds (one per enabled provider) plus the effective
// settings for every known provider (so the API/logs can show disabled ones too).
type Registry struct {
	feeds    map[model.ProviderID]*Feed
	settings map[model.ProviderID]model.ProviderSettings
	ids      []model.ProviderID // all known ids, sorted
}

// NewRegistry builds a Feed for each wired (enabled) reader and retains settings
// for every known id. readers holds only enabled providers.
func NewRegistry(readers map[model.ProviderID]Reader, settings map[model.ProviderID]model.ProviderSettings) *Registry {
	feeds := make(map[model.ProviderID]*Feed, len(readers))
	for id, r := range readers {
		feeds[id] = newFeed(r, settings[id])
	}
	ids := make([]model.ProviderID, 0, len(settings))
	for id := range settings {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return &Registry{feeds: feeds, settings: settings, ids: ids}
}

// Channels returns every enabled feed's channels merged, sorted by provider then
// export number (unnumbered last) then normalized id.
func (r *Registry) Channels() []model.Channel {
	out := make([]model.Channel, 0)
	for _, f := range r.Feeds() {
		out = append(out, f.Channels()...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return lineupLess(
			string(out[i].Provider), string(out[j].Provider),
			out[i].OffsetNumber, out[j].OffsetNumber,
			out[i].NormalizedID, out[j].NormalizedID,
		)
	})
	return out
}

// Feed returns the runtime feed for id, if it is enabled.
func (r *Registry) Feed(id model.ProviderID) (*Feed, bool) {
	f, ok := r.feeds[id]
	return f, ok
}

// Feeds returns the enabled feeds sorted by provider id.
func (r *Registry) Feeds() []*Feed {
	out := make([]*Feed, 0, len(r.feeds))
	for _, id := range r.ids {
		if f, ok := r.feeds[id]; ok {
			out = append(out, f)
		}
	}
	return out
}

// IDs returns the enabled provider ids in sorted order.
func (r *Registry) IDs() []model.ProviderID {
	out := make([]model.ProviderID, 0, len(r.feeds))
	for _, id := range r.ids {
		if _, ok := r.feeds[id]; ok {
			out = append(out, id)
		}
	}
	return out
}

// LogLoaded logs one line per known provider with its effective settings.
func (r *Registry) LogLoaded() {
	for _, p := range r.Providers().Providers {
		slog.Info("provider",
			"id", p.ID,
			"enabled", p.IsEnabled(),
			"label", p.Label,
			"channel_number_offset", p.ChannelNumberOffset,
			"synthesize_channel_numbers", p.SynthesizeChannelNumbers,
			"min_channels", p.MinChannels,
			"refresh_interval", p.RefreshInterval.String(),
			"exclusions", len(p.Exclusions),
			"slug_template", p.SlugTemplate,
			"region", p.Region,
		)
	}
}

// Providers returns all known providers (enabled + disabled) sorted by id, with
// their effective settings — the source for GET /api/providers and startup logs.
func (r *Registry) Providers() model.ProviderList {
	return model.ListProviders(r.settings)
}

// Settings returns the effective settings for id (zero value if unknown).
func (r *Registry) Settings(id model.ProviderID) model.ProviderSettings {
	return r.settings[id]
}

// lineupLess orders lineup rows by provider, then export channel number
// (unnumbered last), then normalized id.
func lineupLess(provI, provJ string, numI, numJ int, idI, idJ string) bool {
	if provI != provJ {
		return provI < provJ
	}
	if numI == 0 {
		numI = 1 << 30
	}
	if numJ == 0 {
		numJ = 1 << 30
	}
	if numI != numJ {
		return numI < numJ
	}
	return idI < idJ
}
