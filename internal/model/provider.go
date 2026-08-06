package model

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

// ProviderSettings holds the config overlay for one known provider (providers.<id>).
// The provider implementations themselves are code (internal/provider/<id>); this
// struct only customizes a known provider: enabled, label, offsets, exclusions,
// URL overrides, etc. YAML that names an unknown id does not create an implementation.
//
// YAML tags load config; JSON tags are the HTTP/API shape. encoding/json does
// not stringify time.Duration, so MarshalJSON/UnmarshalJSON handle
// refresh_interval as a Go duration string and enabled as a resolved bool.
// UnmarshalYAML tracks presence for int fields where 0 is a valid overlay
// (channel_number_offset, synthesize_channel_numbers).
type ProviderSettings struct {
	// ID is the map key (providers.<id>). Not present inside the YAML block.
	ID ProviderID `yaml:"-" json:"id"`

	Enabled *bool `yaml:"enabled" json:"-"` // see MarshalJSON; omitted enabled = true

	Label                    string `yaml:"label" json:"label"`
	ChannelNumberOffset      int    `yaml:"channel_number_offset" json:"channel_number_offset"`
	SynthesizeChannelNumbers int    `yaml:"synthesize_channel_numbers" json:"synthesize_channel_numbers"`
	MinChannels              int    `yaml:"min_channels" json:"min_channels"`

	RefreshInterval time.Duration `yaml:"refresh_interval" json:"-"`

	// ExpectedGuideHorizon is the upstream EPG ahead-depth we expect (code
	// default, not YAML). Used to clamp refresh_interval until an empirical
	// horizon is measured from GuideEnd after a successful fetch.
	ExpectedGuideHorizon time.Duration `yaml:"-" json:"-"`

	// Exclusions are case-insensitive regex patterns matched against stream URL,
	// provider id, and channel name. Compiled by CompileExclusions into ExclusionRegexes.
	Exclusions       []string         `yaml:"exclusions" json:"exclusions,omitempty"`
	ExclusionRegexes []*regexp.Regexp `yaml:"-" json:"-"`

	// SlugTemplate overrides mjh slug construction (e.g. Pluto "plu-{id}.m3u8").
	SlugTemplate string `yaml:"slug_template" json:"slug_template,omitempty"`

	// Region is the effective region list injected from system-wide Config.regions
	// for adapters that honor geography (comma-separated). Per-provider YAML
	// region is ignored; leave empty in overlays. Empty for regionless MJH (Roku).
	Region string `yaml:"region" json:"region,omitempty"`

	// Optional URL overrides (empty = provider built-in defaults).
	ChannelsURL string `yaml:"channels_url" json:"channels_url,omitempty"`
	EPGURL      string `yaml:"epg_url" json:"epg_url,omitempty"`
	M3UURL      string `yaml:"m3u_url" json:"m3u_url,omitempty"`

	// UserAgent is for providers that need a browser-like UA (LocalNow / apsattv).
	UserAgent string `yaml:"user_agent" json:"user_agent,omitempty"`

	// Headers are extra outbound request headers for this provider's fetches.
	Headers map[string]string `yaml:"headers" json:"headers,omitempty"`

	// ChannelEmit customizes what fastgen emits per normalized channel id
	// (presentation + include/exclude). Keys may contain dots; persist as a map.
	ChannelEmit map[string]ChannelEmit `yaml:"channel_emit,omitempty" json:"channel_emit,omitempty"`

	// Presence flags for int overlays where 0 is meaningful (set by UnmarshalYAML).
	channelNumberOffsetSet      bool `yaml:"-" json:"-"`
	synthesizeChannelNumbersSet bool `yaml:"-" json:"-"`
}

// providerSettingsJSON is the explicit JSON wire shape for ProviderSettings.
type providerSettingsJSON struct {
	ID                       ProviderID             `json:"id"`
	Enabled                  bool                   `json:"enabled"`
	Label                    string                 `json:"label"`
	ChannelNumberOffset      int                    `json:"channel_number_offset"`
	SynthesizeChannelNumbers int                    `json:"synthesize_channel_numbers"`
	MinChannels              int                    `json:"min_channels"`
	RefreshInterval          string                 `json:"refresh_interval"`
	ExpectedGuideHorizon     string                 `json:"expected_guide_horizon,omitempty"`
	Exclusions               []string               `json:"exclusions,omitempty"`
	SlugTemplate             string                 `json:"slug_template,omitempty"`
	Region                   string                 `json:"region,omitempty"`
	ChannelsURL              string                 `json:"channels_url,omitempty"`
	EPGURL                   string                 `json:"epg_url,omitempty"`
	M3UURL                   string                 `json:"m3u_url,omitempty"`
	UserAgent                string                 `json:"user_agent,omitempty"`
	Headers                  map[string]string      `json:"headers,omitempty"`
	ChannelEmit              map[string]ChannelEmit `json:"channel_emit,omitempty"`
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
	j := providerSettingsJSON{
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
		ChannelEmit:              p.ChannelEmit,
	}
	if p.ExpectedGuideHorizon > 0 {
		j.ExpectedGuideHorizon = p.ExpectedGuideHorizon.String()
	}
	return json.Marshal(j)
}

// Merge overlays the set fields of o (a YAML overlay) onto p (package defaults)
// and returns the effective settings. Zero-valued overlay fields keep the default,
// except channel_number_offset / synthesize_channel_numbers when YAML set them
// explicitly (including 0). ID is never taken from the overlay.
func (p ProviderSettings) Merge(o ProviderSettings) ProviderSettings {
	if o.Enabled != nil {
		p.Enabled = o.Enabled
	}
	if o.Label != "" {
		p.Label = o.Label
	}
	if o.channelNumberOffsetSet || o.ChannelNumberOffset != 0 {
		p.ChannelNumberOffset = o.ChannelNumberOffset
	}
	if o.synthesizeChannelNumbersSet || o.SynthesizeChannelNumbers != 0 {
		p.SynthesizeChannelNumbers = o.SynthesizeChannelNumbers
	}
	if o.MinChannels != 0 {
		p.MinChannels = o.MinChannels
	}
	if o.RefreshInterval != 0 {
		p.RefreshInterval = o.RefreshInterval
	}
	if o.ExpectedGuideHorizon != 0 {
		p.ExpectedGuideHorizon = o.ExpectedGuideHorizon
	}
	if len(o.Exclusions) > 0 {
		p.Exclusions = o.Exclusions
		p.ExclusionRegexes = o.ExclusionRegexes
	}
	if o.SlugTemplate != "" {
		p.SlugTemplate = o.SlugTemplate
	}
	// Region is system-wide only; never take providers.*.region from overlays.
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
	if o.ChannelEmit != nil {
		p.ChannelEmit = o.ChannelEmit
	}
	return p
}

// MergeConfigured applies an optional provider block and resolves enablement.
// An absent block is disabled; a present block defaults enabled unless it
// explicitly sets enabled: false.
func (p ProviderSettings) MergeConfigured(o ProviderSettings, configured bool) ProviderSettings {
	p = p.Merge(o)
	enabled := configured && o.IsEnabled()
	p.Enabled = &enabled
	return p
}

// Equal reports whether two effective settings are the same operator-visible
// configuration. ExclusionRegexes are derived from Exclusions and ignored;
// Enabled pointers compare by resolved value.
func (p ProviderSettings) Equal(o ProviderSettings) bool {
	if p.IsEnabled() != o.IsEnabled() {
		return false
	}
	if p.ID != o.ID ||
		p.Label != o.Label ||
		p.ChannelNumberOffset != o.ChannelNumberOffset ||
		p.SynthesizeChannelNumbers != o.SynthesizeChannelNumbers ||
		p.MinChannels != o.MinChannels ||
		p.RefreshInterval != o.RefreshInterval ||
		p.ExpectedGuideHorizon != o.ExpectedGuideHorizon ||
		p.SlugTemplate != o.SlugTemplate ||
		p.Region != o.Region ||
		p.ChannelsURL != o.ChannelsURL ||
		p.EPGURL != o.EPGURL ||
		p.M3UURL != o.M3UURL ||
		p.UserAgent != o.UserAgent {
		return false
	}
	return slices.Equal(p.Exclusions, o.Exclusions) &&
		maps.Equal(p.Headers, o.Headers) &&
		ChannelEmitMapsEqual(p.ChannelEmit, o.ChannelEmit)
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

// UnmarshalYAML loads a providers.<id> block and records which int overlays were
// present so Merge can honor explicit zeros (e.g. channel_number_offset: 0).
func (p *ProviderSettings) UnmarshalYAML(value *yaml.Node) error {
	var raw struct {
		Enabled                  *bool                  `yaml:"enabled"`
		Label                    string                 `yaml:"label"`
		ChannelNumberOffset      *int                   `yaml:"channel_number_offset"`
		SynthesizeChannelNumbers *int                   `yaml:"synthesize_channel_numbers"`
		MinChannels              int                    `yaml:"min_channels"`
		RefreshInterval          time.Duration          `yaml:"refresh_interval"`
		Exclusions               []string               `yaml:"exclusions"`
		SlugTemplate             string                 `yaml:"slug_template"`
		Region                   string                 `yaml:"region"`
		ChannelsURL              string                 `yaml:"channels_url"`
		EPGURL                   string                 `yaml:"epg_url"`
		M3UURL                   string                 `yaml:"m3u_url"`
		UserAgent                string                 `yaml:"user_agent"`
		Headers                  map[string]string      `yaml:"headers"`
		ChannelEmit              map[string]ChannelEmit `yaml:"channel_emit"`
	}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*p = ProviderSettings{
		Enabled:         raw.Enabled,
		Label:           raw.Label,
		MinChannels:     raw.MinChannels,
		RefreshInterval: raw.RefreshInterval,
		Exclusions:      raw.Exclusions,
		SlugTemplate:    raw.SlugTemplate,
		Region:          raw.Region,
		ChannelsURL:     raw.ChannelsURL,
		EPGURL:          raw.EPGURL,
		M3UURL:          raw.M3UURL,
		UserAgent:       raw.UserAgent,
		Headers:         raw.Headers,
		ChannelEmit:     raw.ChannelEmit,
	}
	if raw.ChannelNumberOffset != nil {
		p.ChannelNumberOffset = *raw.ChannelNumberOffset
		p.channelNumberOffsetSet = true
	}
	if raw.SynthesizeChannelNumbers != nil {
		p.SynthesizeChannelNumbers = *raw.SynthesizeChannelNumbers
		p.synthesizeChannelNumbersSet = true
	}
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
	var horizon time.Duration
	if j.ExpectedGuideHorizon != "" {
		parsed, err := time.ParseDuration(j.ExpectedGuideHorizon)
		if err != nil {
			return fmt.Errorf("expected_guide_horizon: %w", err)
		}
		horizon = parsed
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
		ExpectedGuideHorizon:     horizon,
		Exclusions:               j.Exclusions,
		SlugTemplate:             j.SlugTemplate,
		Region:                   j.Region,
		ChannelsURL:              j.ChannelsURL,
		EPGURL:                   j.EPGURL,
		M3UURL:                   j.M3UURL,
		UserAgent:                j.UserAgent,
		Headers:                  j.Headers,
		ChannelEmit:              j.ChannelEmit,
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
func ListProviders(byID map[ProviderID]ProviderSettings) ProviderList {
	ids := make([]ProviderID, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]ProviderSettings, 0, len(ids))
	for _, id := range ids {
		p := byID[id]
		p.ID = id
		out = append(out, p)
	}
	return ProviderList{Providers: out}
}
