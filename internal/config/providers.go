package config

import (
	"fmt"
	"log/slog"

	"github.com/j27-aurum/gofast/internal/model"
)

// compileProviders compiles each provider's exclusion regexes. This map is the
// raw YAML overlay only — per-field defaults are owned by the provider packages
// (e.g. lg.DefaultSettings) and merged over this overlay in the bootstrap.
func compileProviders(providers map[model.ProviderID]model.ProviderSettings) (map[model.ProviderID]model.ProviderSettings, error) {
	if len(providers) == 0 {
		return providers, nil
	}
	out := make(map[model.ProviderID]model.ProviderSettings, len(providers))
	for id, p := range providers {
		if p.Region != "" {
			slog.Warn("providers.*.region is ignored; use top-level regions",
				"provider", id, "ignored_region", p.Region)
			p.Region = ""
		}
		if err := p.CompileExclusions(); err != nil {
			return nil, fmt.Errorf("providers.%s: %w", id, err)
		}
		if err := model.ValidateChannelEmitMap(p.ChannelEmit); err != nil {
			return nil, fmt.Errorf("providers.%s: %w", id, err)
		}
		out[id] = p
	}
	return out, nil
}
