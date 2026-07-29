package model

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Channel is a lineup entry after provider fetch (+ optional classify).
type Channel struct {
	Provider    ProviderID `json:"provider"`
	ID          string     `json:"id"` // upstream id before Normalize
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Group       string     `json:"group"`
	Number      int        `json:"number"`                // provider's upstream channel number
	StreamURL   string     `json:"stream_url"`            // provider's upstream URL
	EmittedURL  string     `json:"emitted_url,omitempty"` // selected direct/proxy playback URL
	// EmittedName is the operator-customized display name for M3U/XMLTV. Empty
	// means export uses Name. Upstream Name is never mutated.
	EmittedName string `json:"emitted_name,omitempty"`
	// EmittedGroup is the resolved M3U group-title after the group taxonomy
	// (merge / drop-prefix). Empty means "use the legacy {label}: {group}".
	// Upstream Group is never mutated so diagnosis still shows the source group.
	EmittedGroup string `json:"emitted_group,omitempty"`
	// LogoURL is what we export (tvg-logo / XMLTV icon): local /logos/... when
	// caching succeeds, upstream on soft failure / cache off, empty after hard 403/404.
	LogoURL string `json:"logo_url,omitempty"`
	// LogoSourceURL is the provider's original artwork URL (kept after rewrite).
	LogoSourceURL string `json:"logo_source_url,omitempty"`
	// LogoError is set when logo caching cleared LogoURL after a hard upstream
	// failure (e.g. HTTP 403/404). Empty when the logo is fine or caching is off.
	LogoError string `json:"logo_error,omitempty"`

	// OffsetNumber is Number + provider channel_number_offset (export / tvg-chno / lcn).
	// Zero when the upstream had no number (synthesize path handles it later).
	OffsetNumber int `json:"offset_number"`

	// NormalizedID is NormalizeID(ID) — same value for M3U tvg-id, XMLTV channel id,
	// and programme channel references.
	NormalizedID string `json:"normalized_id"`

	Classification Classification `json:"classification,omitempty"`
	LicenseURL     string         `json:"license_url,omitempty"`
	// Health is the current playability stamp (from channel-attr store Annotate).
	// Zero / empty status means untested.
	Health ChannelHealth `json:"health"`
	// FilterReason is the primary (badge-driving) exclusion reason.
	FilterReason FilterReason `json:"filter_reason,omitempty"`
	// FilterReasons lists every applicable exclusion reason (may be multiple).
	FilterReasons []FilterReason `json:"filter_reasons,omitempty"`
	Excluded      bool           `json:"excluded"`

	// Emit is the configured channel_emit row for this channel (API paint only).
	Emit *ChannelEmit `json:"emit,omitempty"`
	// EmitDefaults are fastgen-produced export values before per-field customs.
	EmitDefaults *EmitDefaults `json:"emit_defaults,omitempty"`

	// ForceInclude is set when export:enabled so emission skips exclude_unhealthy.
	ForceInclude bool `json:"-"`

	// RequestHeaders are provider-supplied headers used for stream probes and,
	// later, logo retrieval. They are operational metadata, not part of our API.
	RequestHeaders map[string]string `json:"-"`
}

// MatchesExclusion reports whether any compiled regex matches stream URL, provider id, or name.
// Patterns are expected already compiled with (?i) as config does at load.
// When multiple regexes match, reasons lists each distinct ExclusionMatched value.
func (c Channel) MatchesExclusion(regexes []*regexp.Regexp) (matched bool, reasons []FilterReason) {
	haystacks := []string{c.StreamURL, string(c.Provider), c.ID, c.Name, c.NormalizedID}
	for _, re := range regexes {
		if re == nil {
			continue
		}
		for _, h := range haystacks {
			if h != "" && re.MatchString(h) {
				r := ExclusionMatched(re)
				if !HasFilterReason(reasons, r) {
					reasons = append(reasons, r)
				}
				matched = true
				break
			}
		}
	}
	return matched, reasons
}

// OutputURL returns the URL written to playlists while StreamURL remains the
// provider's upstream URL for diagnostics and future FASTProxy lookup.
func (c Channel) OutputURL() string {
	if c.EmittedURL != "" {
		return c.EmittedURL
	}
	return c.StreamURL
}

// ApplyChannelNumberOffset sets OffsetNumber to Number+offset for export.
// Number is left as the provider's upstream value. Number <= 0 means "no upstream
// number" — Number and OffsetNumber are forced to 0 (synthesize path handles it).
func (c *Channel) ApplyChannelNumberOffset(offset int) {
	if c == nil {
		return
	}
	if c.Number <= 0 {
		c.Number = 0
		c.OffsetNumber = 0
		return
	}
	c.OffsetNumber = c.Number + offset
}

// Normalize fills NormalizedID and trims display fields. Presentation escaping
// remains the responsibility of the output format.
func (c *Channel) Normalize() {
	if c == nil {
		return
	}
	c.ID = strings.TrimSpace(c.ID)
	c.NormalizedID = NormalizeID(c.ID)
	c.Name = strings.TrimSpace(c.Name)
	c.Group = strings.TrimSpace(c.Group)
}

// ForExport returns channels that belong in M3U/XMLTV playlists.
// Excluded channels are omitted (identity/stream gaps should already be reasons).
func ForExport(channels []Channel) []Channel {
	out := make([]Channel, 0, len(channels))
	for _, ch := range channels {
		if ch.Excluded || ch.NormalizedID == "" || ch.OutputURL() == "" {
			continue
		}
		out = append(out, ch)
	}
	return out
}

// MarkExclusions marks channels that match any provider exclusion regex.
// Excluded channels stay in the slice with FilterReasons set so the UI can explain drops.
func MarkExclusions(channels []Channel, regexes []*regexp.Regexp) []Channel {
	if len(regexes) == 0 {
		return channels
	}
	out := make([]Channel, len(channels))
	copy(out, channels)
	for i := range out {
		if ok, reasons := out[i].MatchesExclusion(regexes); ok {
			for _, r := range reasons {
				out[i].AddFilterReason(r)
			}
		}
	}
	return out
}

// allowedID keeps [A-Za-z0-9._-] after whitespace→_ and quote/control stripping.
var allowedID = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// NormalizeID normalizes a channel id for M3U tvg-id, XMLTV <channel id>, and programme channel.
// Deterministic: same input always yields the same output.
func NormalizeID(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		switch {
		case unicode.IsSpace(r):
			b.WriteByte('_')
		case r == '"' || r == '\'':
			// strip quotes
		case unicode.IsControl(r):
			// strip controls
		default:
			b.WriteRune(r)
		}
	}
	s := allowedID.ReplaceAllString(b.String(), "")
	for strings.Contains(s, "__") {
		s = strings.ReplaceAll(s, "__", "_")
	}
	return strings.Trim(s, "_")
}

// ValidateNormalizedIDs rejects ambiguous channel identities after
// normalization. Distinct upstream ids must never collapse to one M3U/XMLTV
// join key.
func ValidateNormalizedIDs(channels []Channel) error {
	rawByNormalized := make(map[string]string, len(channels))
	for _, channel := range channels {
		if channel.NormalizedID == "" {
			continue
		}
		if previous, exists := rawByNormalized[channel.NormalizedID]; exists {
			return fmt.Errorf(
				"normalized channel id collision %q from upstream ids %q and %q",
				channel.NormalizedID,
				previous,
				channel.ID,
			)
		}
		rawByNormalized[channel.NormalizedID] = channel.ID
	}
	return nil
}
