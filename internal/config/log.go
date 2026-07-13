package config

import (
	"log/slog"

	"github.com/j27-aurum/gofast/internal/model"
)

// LogLoaded writes structured startup lines for this config.
func (c *Config) LogLoaded(path string, fromFile bool) {
	if c == nil {
		return
	}
	slog.Info("config loaded",
		"path", path,
		"from_file", fromFile,
		"listen", c.Listen,
		"base_url", c.BaseURL,
		"data_dir", c.DataDir,
		"provider_count", len(c.Providers),
	)
	if len(c.Providers) == 0 {
		slog.Warn("no providers configured; copy config.example.yaml to the data volume as config.yaml")
		return
	}
	for _, p := range model.ListProviders(c.Providers).Providers {
		slog.Info("provider",
			"id", p.ID,
			"enabled", p.IsEnabled(),
			"label", p.Label,
			"chno_offset", p.ChnoOffset,
			"synthesize_chno", p.SynthesizeChno,
			"min_channels", p.MinChannels,
			"refresh_interval", p.RefreshInterval.String(),
			"exclusions", len(p.Exclusions),
			"slug_template", p.SlugTemplate,
			"region", p.Region,
		)
	}
}
