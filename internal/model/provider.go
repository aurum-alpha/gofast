package model

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"time"
)

// Provider is a configured FAST lineup source from YAML (providers.<id>).
// Adapters implement provider.Reader to fetch data for a Provider.
//
// YAML tags load config; JSON tags are the HTTP/API shape. encoding/json does
// not stringify time.Duration, so MarshalJSON/UnmarshalJSON handle
// refresh_interval as a Go duration string and enabled as a resolved bool.
type Provider struct {
	// ID is the map key (providers.<id>). Not present inside the YAML block.
	ID string `yaml:"-" json:"id"`

	Enabled *bool `yaml:"enabled" json:"-"` // see MarshalJSON; omitted enabled = true

	Label          string `yaml:"label" json:"label"`
	ChnoOffset     int    `yaml:"chno_offset" json:"chno_offset"`
	SynthesizeChno int    `yaml:"synthesize_chno" json:"synthesize_chno"`
	MinChannels    int    `yaml:"min_channels" json:"min_channels"`

	RefreshInterval time.Duration `yaml:"refresh_interval" json:"-"`

	// Exclusions are case-insensitive regex patterns matched against stream URL,
	// provider id, and channel name. Compiled at load into ExclusionRegexes.
	Exclusions       []string         `yaml:"exclusions" json:"exclusions,omitempty"`
	ExclusionRegexes []*regexp.Regexp `yaml:"-" json:"-"`

	// SlugTemplate overrides mjh slug construction (e.g. Pluto "plu-{id}.m3u8").
	SlugTemplate string `yaml:"slug_template" json:"slug_template,omitempty"`

	// Region selects mjh regioned feeds (e.g. "us"). Empty for regionless (Roku).
	Region string `yaml:"region" json:"region,omitempty"`

	// Optional URL overrides (empty = adapter built-in defaults).
	ChannelsURL string `yaml:"channels_url" json:"channels_url,omitempty"`
	EPGURL      string `yaml:"epg_url" json:"epg_url,omitempty"`
	M3UURL      string `yaml:"m3u_url" json:"m3u_url,omitempty"`

	// UserAgent is for adapters that need a browser-like UA (LocalNow / apsattv).
	UserAgent string `yaml:"user_agent" json:"user_agent,omitempty"`

	// Headers are extra outbound request headers for this provider's fetches.
	Headers map[string]string `yaml:"headers" json:"headers,omitempty"`
}

// providerJSON is the explicit JSON wire shape for Provider.
type providerJSON struct {
	ID              string            `json:"id"`
	Enabled         bool              `json:"enabled"`
	Label           string            `json:"label"`
	ChnoOffset      int               `json:"chno_offset"`
	SynthesizeChno  int               `json:"synthesize_chno"`
	MinChannels     int               `json:"min_channels"`
	RefreshInterval string            `json:"refresh_interval"`
	Exclusions      []string          `json:"exclusions,omitempty"`
	SlugTemplate    string            `json:"slug_template,omitempty"`
	Region          string            `json:"region,omitempty"`
	ChannelsURL     string            `json:"channels_url,omitempty"`
	EPGURL          string            `json:"epg_url,omitempty"`
	M3UURL          string            `json:"m3u_url,omitempty"`
	UserAgent       string            `json:"user_agent,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
}

// MarshalJSON encodes refresh_interval as a Go duration string and enabled as IsEnabled().
func (p Provider) MarshalJSON() ([]byte, error) {
	return json.Marshal(providerJSON{
		ID:              p.ID,
		Enabled:         p.IsEnabled(),
		Label:           p.Label,
		ChnoOffset:      p.ChnoOffset,
		SynthesizeChno:  p.SynthesizeChno,
		MinChannels:     p.MinChannels,
		RefreshInterval: p.RefreshInterval.String(),
		Exclusions:      p.Exclusions,
		SlugTemplate:    p.SlugTemplate,
		Region:          p.Region,
		ChannelsURL:     p.ChannelsURL,
		EPGURL:          p.EPGURL,
		M3UURL:          p.M3UURL,
		UserAgent:       p.UserAgent,
		Headers:         p.Headers,
	})
}

// UnmarshalJSON decodes the API/JSON shape into Provider.
func (p *Provider) UnmarshalJSON(data []byte) error {
	var j providerJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	var d time.Duration
	if j.RefreshInterval != "" {
		parsed, err := time.ParseDuration(j.RefreshInterval)
		if err != nil {
			return fmt.Errorf("refresh_interval: %w", err)
		}
		d = parsed
	}
	enabled := j.Enabled
	*p = Provider{
		ID:              j.ID,
		Enabled:         &enabled,
		Label:           j.Label,
		ChnoOffset:      j.ChnoOffset,
		SynthesizeChno:  j.SynthesizeChno,
		MinChannels:     j.MinChannels,
		RefreshInterval: d,
		Exclusions:      j.Exclusions,
		SlugTemplate:    j.SlugTemplate,
		Region:          j.Region,
		ChannelsURL:     j.ChannelsURL,
		EPGURL:          j.EPGURL,
		M3UURL:          j.M3UURL,
		UserAgent:       j.UserAgent,
		Headers:         j.Headers,
	}
	return nil
}

// IsEnabled reports whether the provider should run. Omitted enabled defaults to true.
func (p Provider) IsEnabled() bool {
	if p.Enabled == nil {
		return true
	}
	return *p.Enabled
}

// DefaultProvider returns per-field defaults applied at config load.
func DefaultProvider() Provider {
	return Provider{
		MinChannels:     1,
		RefreshInterval: 6 * time.Hour,
	}
}

// MergeProviderDefaults fills zero-valued fields from DefaultProvider.
func MergeProviderDefaults(p Provider) Provider {
	d := DefaultProvider()
	if p.MinChannels == 0 {
		p.MinChannels = d.MinChannels
	}
	if p.RefreshInterval == 0 {
		p.RefreshInterval = d.RefreshInterval
	}
	return p
}

// ProviderList is the GET /api/providers JSON envelope (providers only — no server config).
type ProviderList struct {
	Providers []Provider `json:"providers"`
}

// ListProviders returns providers sorted by id, with ID set from each map key.
func ListProviders(byID map[string]Provider) ProviderList {
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Provider, 0, len(ids))
	for _, id := range ids {
		p := byID[id]
		p.ID = id
		out = append(out, p)
	}
	return ProviderList{Providers: out}
}
