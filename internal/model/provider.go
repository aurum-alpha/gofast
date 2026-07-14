package model

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"time"
)

// ProviderSettings holds the config overlay for one known provider (providers.<id>).
// The provider implementations themselves are code (internal/provider/<id>); this
// struct only customizes a known provider: enabled, label, offsets, exclusions,
// URL overrides, etc. YAML that names an unknown id does not create an implementation.
//
// YAML tags load config; JSON tags are the HTTP/API shape. encoding/json does
// not stringify time.Duration, so MarshalJSON/UnmarshalJSON handle
// refresh_interval as a Go duration string and enabled as a resolved bool.
type ProviderSettings struct {
	// ID is the map key (providers.<id>). Not present inside the YAML block.
	ID string `yaml:"-" json:"id"`

	Enabled *bool `yaml:"enabled" json:"-"` // see MarshalJSON; omitted enabled = true

	Label                    string `yaml:"label" json:"label"`
	ChannelNumberOffset      int    `yaml:"channel_number_offset" json:"channel_number_offset"`
	SynthesizeChannelNumbers int    `yaml:"synthesize_channel_numbers" json:"synthesize_channel_numbers"`
	MinChannels              int    `yaml:"min_channels" json:"min_channels"`

	RefreshInterval time.Duration `yaml:"refresh_interval" json:"-"`

	// Exclusions are case-insensitive regex patterns matched against stream URL,
	// provider id, and channel name. Compiled by CompileExclusions into ExclusionRegexes.
	Exclusions       []string         `yaml:"exclusions" json:"exclusions,omitempty"`
	ExclusionRegexes []*regexp.Regexp `yaml:"-" json:"-"`

	// SlugTemplate overrides mjh slug construction (e.g. Pluto "plu-{id}.m3u8").
	SlugTemplate string `yaml:"slug_template" json:"slug_template,omitempty"`

	// Region selects mjh regioned feeds (e.g. "us"). Empty for regionless (Roku).
	Region string `yaml:"region" json:"region,omitempty"`

	// Optional URL overrides (empty = provider built-in defaults).
	ChannelsURL string `yaml:"channels_url" json:"channels_url,omitempty"`
	EPGURL      string `yaml:"epg_url" json:"epg_url,omitempty"`
	M3UURL      string `yaml:"m3u_url" json:"m3u_url,omitempty"`

	// UserAgent is for providers that need a browser-like UA (LocalNow / apsattv).
	UserAgent string `yaml:"user_agent" json:"user_agent,omitempty"`

	// Headers are extra outbound request headers for this provider's fetches.
	Headers map[string]string `yaml:"headers" json:"headers,omitempty"`
}

// providerSettingsJSON is the explicit JSON wire shape for ProviderSettings.
type providerSettingsJSON struct {
	ID                       string            `json:"id"`
	Enabled                  bool              `json:"enabled"`
	Label                    string            `json:"label"`
	ChannelNumberOffset      int               `json:"channel_number_offset"`
	SynthesizeChannelNumbers int               `json:"synthesize_channel_numbers"`
	MinChannels              int               `json:"min_channels"`
	RefreshInterval          string            `json:"refresh_interval"`
	Exclusions               []string          `json:"exclusions,omitempty"`
	SlugTemplate             string            `json:"slug_template,omitempty"`
	Region                   string            `json:"region,omitempty"`
	ChannelsURL              string            `json:"channels_url,omitempty"`
	EPGURL                   string            `json:"epg_url,omitempty"`
	M3UURL                   string            `json:"m3u_url,omitempty"`
	UserAgent                string            `json:"user_agent,omitempty"`
	Headers                  map[string]string `json:"headers,omitempty"`
}

// IsEnabled reports whether the provider should run. Omitted enabled defaults to true.
func (p ProviderSettings) IsEnabled() bool {
	if p.Enabled == nil {
		return true
	}
	return *p.Enabled
}

// MarshalJSON encodes refresh_interval as a Go duration string and enabled as IsEnabled().
func (p ProviderSettings) MarshalJSON() ([]byte, error) {
	return json.Marshal(providerSettingsJSON{
		ID:                       p.ID,
		Enabled:                  p.IsEnabled(),
		Label:                    p.Label,
		ChannelNumberOffset:      p.ChannelNumberOffset,
		SynthesizeChannelNumbers: p.SynthesizeChannelNumbers,
		MinChannels:              p.MinChannels,
		RefreshInterval:          p.RefreshInterval.String(),
		Exclusions:               p.Exclusions,
		SlugTemplate:             p.SlugTemplate,
		Region:                   p.Region,
		ChannelsURL:              p.ChannelsURL,
		EPGURL:                   p.EPGURL,
		M3UURL:                   p.M3UURL,
		UserAgent:                p.UserAgent,
		Headers:                  p.Headers,
	})
}

// Merge overlays the set fields of o (a YAML overlay) onto p (package defaults)
// and returns the effective settings. Zero-valued overlay fields keep the default.
// ID is never taken from the overlay (it lives on the map key / package default).
func (p ProviderSettings) Merge(o ProviderSettings) ProviderSettings {
	if o.Enabled != nil {
		p.Enabled = o.Enabled
	}
	if o.Label != "" {
		p.Label = o.Label
	}
	if o.ChannelNumberOffset != 0 {
		p.ChannelNumberOffset = o.ChannelNumberOffset
	}
	if o.SynthesizeChannelNumbers != 0 {
		p.SynthesizeChannelNumbers = o.SynthesizeChannelNumbers
	}
	if o.MinChannels != 0 {
		p.MinChannels = o.MinChannels
	}
	if o.RefreshInterval != 0 {
		p.RefreshInterval = o.RefreshInterval
	}
	if len(o.Exclusions) > 0 {
		p.Exclusions = o.Exclusions
		p.ExclusionRegexes = o.ExclusionRegexes
	}
	if o.SlugTemplate != "" {
		p.SlugTemplate = o.SlugTemplate
	}
	if o.Region != "" {
		p.Region = o.Region
	}
	if o.ChannelsURL != "" {
		p.ChannelsURL = o.ChannelsURL
	}
	if o.EPGURL != "" {
		p.EPGURL = o.EPGURL
	}
	if o.M3UURL != "" {
		p.M3UURL = o.M3UURL
	}
	if o.UserAgent != "" {
		p.UserAgent = o.UserAgent
	}
	if len(o.Headers) > 0 {
		p.Headers = o.Headers
	}
	return p
}

// CompileExclusions compiles Exclusions into ExclusionRegexes (case-insensitive).
func (p *ProviderSettings) CompileExclusions() error {
	compiled := make([]*regexp.Regexp, 0, len(p.Exclusions))
	for i, pat := range p.Exclusions {
		re, err := regexp.Compile("(?i)" + pat)
		if err != nil {
			return fmt.Errorf("exclusions[%d]: invalid regex %q: %w", i, pat, err)
		}
		compiled = append(compiled, re)
	}
	p.ExclusionRegexes = compiled
	return nil
}

// UnmarshalJSON decodes the API/JSON shape into ProviderSettings.
func (p *ProviderSettings) UnmarshalJSON(data []byte) error {
	var j providerSettingsJSON
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
	*p = ProviderSettings{
		ID:                       j.ID,
		Enabled:                  &enabled,
		Label:                    j.Label,
		ChannelNumberOffset:      j.ChannelNumberOffset,
		SynthesizeChannelNumbers: j.SynthesizeChannelNumbers,
		MinChannels:              j.MinChannels,
		RefreshInterval:          d,
		Exclusions:               j.Exclusions,
		SlugTemplate:             j.SlugTemplate,
		Region:                   j.Region,
		ChannelsURL:              j.ChannelsURL,
		EPGURL:                   j.EPGURL,
		M3UURL:                   j.M3UURL,
		UserAgent:                j.UserAgent,
		Headers:                  j.Headers,
	}
	return nil
}

// DefaultSettings returns the baseline per-field defaults a provider package
// builds on (e.g. lg.DefaultSettings starts here, then sets its own fields).
func DefaultSettings() ProviderSettings {
	return ProviderSettings{
		MinChannels:     1,
		RefreshInterval: 6 * time.Hour,
	}
}

// ProviderList is the GET /api/providers JSON envelope (providers only — no server config).
type ProviderList struct {
	Providers []ProviderSettings `json:"providers"`
}

// ListProviders returns providers sorted by id, with ID set from each map key.
func ListProviders(byID map[string]ProviderSettings) ProviderList {
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]ProviderSettings, 0, len(ids))
	for _, id := range ids {
		p := byID[id]
		p.ID = id
		out = append(out, p)
	}
	return ProviderList{Providers: out}
}
