package config

import (
	"log/slog"
	"sort"
	"time"
)

// ProviderView is a JSON-safe snapshot of one provider for logs and the API.
type ProviderView struct {
	ID             string            `json:"id"`
	Enabled        bool              `json:"enabled"`
	Label          string            `json:"label"`
	ChnoOffset     int               `json:"chno_offset"`
	SynthesizeChno int               `json:"synthesize_chno"`
	MinChannels    int               `json:"min_channels"`
	Refresh        string            `json:"refresh_interval"`
	Exclusions     int               `json:"exclusions"`
	SlugTemplate   string            `json:"slug_template,omitempty"`
	Region         string            `json:"region,omitempty"`
	ChannelsURL    string            `json:"channels_url,omitempty"`
	EPGURL         string            `json:"epg_url,omitempty"`
	M3UURL         string            `json:"m3u_url,omitempty"`
	UserAgent      string            `json:"user_agent,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
}

// ProvidersView is the GET /api/providers payload.
type ProvidersView struct {
	Path      string         `json:"path"`
	FromFile  bool           `json:"from_file"`
	Listen    string         `json:"listen"`
	BaseURL   string         `json:"base_url"`
	DataDir   string         `json:"data_dir"`
	Providers []ProviderView `json:"providers"`
}

// ViewProviders builds a stable, sorted snapshot of cfg for API/UI.
func ViewProviders(path string, fromFile bool, cfg Config) ProvidersView {
	ids := make([]string, 0, len(cfg.Providers))
	for id := range cfg.Providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]ProviderView, 0, len(ids))
	for _, id := range ids {
		p := cfg.Providers[id]
		refresh := p.RefreshInterval.Duration().String()
		if p.RefreshInterval == 0 {
			refresh = (6 * time.Hour).String()
		}
		out = append(out, ProviderView{
			ID:             id,
			Enabled:        p.IsEnabled(),
			Label:          p.Label,
			ChnoOffset:     p.ChnoOffset,
			SynthesizeChno: p.SynthesizeChno,
			MinChannels:    p.MinChannels,
			Refresh:        refresh,
			Exclusions:     len(p.Exclusions),
			SlugTemplate:   p.SlugTemplate,
			Region:         p.Region,
			ChannelsURL:    p.ChannelsURL,
			EPGURL:         p.EPGURL,
			M3UURL:         p.M3UURL,
			UserAgent:      p.UserAgent,
			Headers:        p.Headers,
		})
	}
	return ProvidersView{
		Path:      path,
		FromFile:  fromFile,
		Listen:    cfg.Listen,
		BaseURL:   cfg.BaseURL,
		DataDir:   cfg.DataDir,
		Providers: out,
	}
}

// LogLoaded writes structured startup lines for the loaded config.
func LogLoaded(path string, fromFile bool, cfg Config) {
	view := ViewProviders(path, fromFile, cfg)
	slog.Info("config loaded",
		"path", view.Path,
		"from_file", view.FromFile,
		"listen", view.Listen,
		"base_url", view.BaseURL,
		"data_dir", view.DataDir,
		"provider_count", len(view.Providers),
	)
	if len(view.Providers) == 0 {
		slog.Warn("no providers configured; copy config.example.yaml to the data volume as config.yaml")
		return
	}
	for _, p := range view.Providers {
		slog.Info("provider",
			"id", p.ID,
			"enabled", p.Enabled,
			"label", p.Label,
			"chno_offset", p.ChnoOffset,
			"synthesize_chno", p.SynthesizeChno,
			"min_channels", p.MinChannels,
			"refresh_interval", p.Refresh,
			"exclusions", p.Exclusions,
			"slug_template", p.SlugTemplate,
			"region", p.Region,
		)
	}
}
