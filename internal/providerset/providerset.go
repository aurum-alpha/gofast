// Package providerset is the catalog of shipped provider implementations: it
// maps known provider ids to their package constructors and defaults, and
// describes which optional settings fields each adapter actually reads (so the
// UI only offers controls a provider honors). Adding a provider means adding
// its package here — YAML alone never creates an implementation.
package providerset

import (
	"log/slog"
	"sort"

	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/provider/distrotv"
	"github.com/j27-aurum/gofast/internal/provider/lg"
	"github.com/j27-aurum/gofast/internal/provider/localnow"
	"github.com/j27-aurum/gofast/internal/provider/plex"
	"github.com/j27-aurum/gofast/internal/provider/pluto"
	"github.com/j27-aurum/gofast/internal/provider/roku"
	"github.com/j27-aurum/gofast/internal/provider/samsung"
	"github.com/j27-aurum/gofast/internal/provider/tcl"
	"github.com/j27-aurum/gofast/internal/provider/tubi"
	"github.com/j27-aurum/gofast/internal/provider/xumo"
)

// entry is one shipped provider implementation.
type entry struct {
	defaults func() model.ProviderSettings
	reader   func(model.ProviderSettings, *httpx.Client) provider.Reader
	// fields are the optional per-provider settings this adapter reads, using
	// the config key names (region, slug_template, channels_url, epg_url,
	// m3u_url, user_agent, headers). Core fields (enabled, label, offsets,
	// refresh_interval, exclusions) apply to every provider.
	fields []string
}

var catalog = map[model.ProviderID]entry{
	model.ProviderDistroTV: {
		defaults: distrotv.DefaultSettings,
		reader:   func(s model.ProviderSettings, c *httpx.Client) provider.Reader { return distrotv.New(s, c) },
		// region = Distro geo code (QQ, US, …); channels_url/epg_url override jsrdn endpoints.
		fields: []string{"region", "channels_url", "epg_url", "user_agent", "headers"},
	},
	model.ProviderLG: {
		defaults: lg.DefaultSettings,
		reader:   func(s model.ProviderSettings, c *httpx.Client) provider.Reader { return lg.New(s, c) },
		fields:   []string{"channels_url", "user_agent", "headers"},
	},
	model.ProviderLocalNow: {
		defaults: localnow.DefaultSettings,
		reader:   func(s model.ProviderSettings, c *httpx.Client) provider.Reader { return localnow.New(s, c) },
		fields:   []string{"m3u_url", "epg_url", "user_agent", "headers"},
	},
	model.ProviderPlex: {
		defaults: plex.DefaultSettings,
		reader:   func(s model.ProviderSettings, c *httpx.Client) provider.Reader { return plex.New(s, c) },
		fields:   []string{"region", "slug_template", "channels_url", "epg_url", "headers"},
	},
	model.ProviderPluto: {
		defaults: pluto.DefaultSettings,
		reader:   func(s model.ProviderSettings, c *httpx.Client) provider.Reader { return pluto.New(s, c) },
		fields:   []string{"region", "slug_template", "channels_url", "epg_url", "headers"},
	},
	model.ProviderRoku: {
		defaults: roku.DefaultSettings,
		reader:   func(s model.ProviderSettings, c *httpx.Client) provider.Reader { return roku.New(s, c) },
		fields:   []string{"slug_template", "channels_url", "epg_url", "headers"},
	},
	model.ProviderSamsung: {
		defaults: samsung.DefaultSettings,
		reader:   func(s model.ProviderSettings, c *httpx.Client) provider.Reader { return samsung.New(s, c) },
		fields:   []string{"region", "slug_template", "channels_url", "epg_url", "headers"},
	},
	model.ProviderTCL: {
		defaults: tcl.DefaultSettings,
		reader:   func(s model.ProviderSettings, c *httpx.Client) provider.Reader { return tcl.New(s, c) },
		fields:   []string{"m3u_url", "epg_url", "user_agent", "headers"},
	},
	model.ProviderTubi: {
		defaults: tubi.DefaultSettings,
		reader:   func(s model.ProviderSettings, c *httpx.Client) provider.Reader { return tubi.New(s, c) },
		fields:   []string{"m3u_url", "epg_url", "user_agent", "headers"},
	},
	model.ProviderXumo: {
		defaults: xumo.DefaultSettings,
		reader:   func(s model.ProviderSettings, c *httpx.Client) provider.Reader { return xumo.New(s, c) },
		fields:   []string{"m3u_url", "epg_url", "user_agent", "headers"},
	},
}

// Known returns all shipped provider ids, sorted.
func Known() []model.ProviderID {
	out := make([]model.ProviderID, 0, len(catalog))
	for id := range catalog {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// FieldSupport returns the optional settings field names the adapter for id
// reads (nil for unknown ids).
func FieldSupport(id model.ProviderID) []string {
	e, ok := catalog[id]
	if !ok {
		return nil
	}
	return append([]string(nil), e.fields...)
}

// Reader builds the fetch adapter for a known provider with its effective
// settings. ok is false for unknown ids.
func Reader(id model.ProviderID, settings model.ProviderSettings, client *httpx.Client) (provider.Reader, bool) {
	e, ok := catalog[id]
	if !ok {
		return nil, false
	}
	return e.reader(settings, client), true
}

// Readers builds one Reader per enabled provider in settings.
func Readers(settings map[model.ProviderID]model.ProviderSettings, client *httpx.Client) map[model.ProviderID]provider.Reader {
	readers := map[model.ProviderID]provider.Reader{}
	for _, id := range Known() {
		s := settings[id]
		if !s.IsEnabled() {
			slog.Info("provider disabled", "id", id)
			continue
		}
		if r, ok := Reader(id, s, client); ok {
			readers[id] = r
		}
	}
	return readers
}

// Settings overlays each known provider's package defaults with its optional
// YAML block and resolves enablement: no block means disabled, a present block
// defaults enabled unless it sets enabled: false. Unknown overlay ids are
// warned and ignored (implementations are code, not YAML).
func Settings(overlays map[model.ProviderID]model.ProviderSettings) map[model.ProviderID]model.ProviderSettings {
	out := make(map[model.ProviderID]model.ProviderSettings, len(catalog))
	for id, e := range catalog {
		overlay, configured := overlays[id]
		out[id] = e.defaults().MergeConfigured(overlay, configured)
	}
	for id := range overlays {
		if _, known := catalog[id]; !known {
			slog.Warn("provider in config has no implementation; ignoring", "id", id)
		}
	}
	return out
}
