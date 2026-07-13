package config

import (
	"fmt"
	"regexp"
	"time"
)

// Provider is per-provider settings (YAML under providers.<id>).
type Provider struct {
	Enabled *bool `yaml:"enabled"`

	Label          string `yaml:"label"`
	ChnoOffset     int    `yaml:"chno_offset"`
	SynthesizeChno int    `yaml:"synthesize_chno"` // 0 = off; otherwise first-seen base for id→chno map
	MinChannels    int    `yaml:"min_channels"`

	RefreshInterval Duration `yaml:"refresh_interval"`

	// Exclusions are case-insensitive regex patterns matched against stream URL,
	// provider id, and channel name. Compiled at load into ExclusionRegexes.
	Exclusions       []string         `yaml:"exclusions"`
	ExclusionRegexes []*regexp.Regexp `yaml:"-"`

	// SlugTemplate overrides mjh slug construction (e.g. Pluto "plu-{id}.m3u8").
	SlugTemplate string `yaml:"slug_template"`

	// Region selects mjh regioned feeds (e.g. "us"). Empty for regionless (Roku).
	Region string `yaml:"region"`

	// Optional URL overrides (empty = adapter built-in defaults).
	ChannelsURL string `yaml:"channels_url"`
	EPGURL      string `yaml:"epg_url"`
	M3UURL      string `yaml:"m3u_url"`

	// UserAgent is for adapters that need a browser-like UA (LocalNow / apsattv).
	UserAgent string `yaml:"user_agent"`

	// Headers are extra outbound request headers for this provider's fetches.
	Headers map[string]string `yaml:"headers"`
}

// IsEnabled reports whether the provider should run. Omitted enabled defaults to true.
func (p Provider) IsEnabled() bool {
	if p.Enabled == nil {
		return true
	}
	return *p.Enabled
}

const defaultRefreshInterval = Duration(6 * time.Hour)

func defaultProvider() Provider {
	return Provider{
		MinChannels:     1,
		RefreshInterval: defaultRefreshInterval,
	}
}

func mergeProviderDefaults(p Provider) Provider {
	d := defaultProvider()
	if p.MinChannels == 0 {
		p.MinChannels = d.MinChannels
	}
	if p.RefreshInterval == 0 {
		p.RefreshInterval = d.RefreshInterval
	}
	return p
}

// compileProviders applies per-field defaults and compiles exclusion regexes.
// There are no built-in provider identities — the map comes only from YAML.
func compileProviders(providers map[string]Provider) (map[string]Provider, error) {
	if len(providers) == 0 {
		return providers, nil
	}
	out := make(map[string]Provider, len(providers))
	for id, p := range providers {
		p = mergeProviderDefaults(p)
		compiled := make([]*regexp.Regexp, 0, len(p.Exclusions))
		for i, pat := range p.Exclusions {
			re, err := regexp.Compile("(?i)" + pat)
			if err != nil {
				return nil, fmt.Errorf("providers.%s.exclusions[%d]: invalid regex %q: %w", id, i, pat, err)
			}
			compiled = append(compiled, re)
		}
		p.ExclusionRegexes = compiled
		out[id] = p
	}
	return out, nil
}
