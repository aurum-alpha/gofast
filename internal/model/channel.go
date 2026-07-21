package model

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/j27-aurum/gofast/internal/format"
)

// Channel is a lineup entry after provider fetch (+ optional classify).
type Channel struct {
	Provider  ProviderID `json:"provider"`
	ID        string     `json:"id"` // upstream id before Normalize
	Name      string     `json:"name"`
	Group     string     `json:"group"`
	Number    int        `json:"number"` // provider's upstream channel number
	StreamURL string     `json:"stream_url"`
	LogoURL   string     `json:"logo_url,omitempty"`

	// OffsetNumber is Number + provider channel_number_offset (export / tvg-chno / lcn).
	// Zero when the upstream had no number (synthesize path handles it later).
	OffsetNumber int `json:"offset_number"`

	// NormalizedID is NormalizeID(ID) — same value for M3U tvg-id, XMLTV channel id,
	// and programme channel references.
	NormalizedID string `json:"normalized_id"`

	Classification Classification `json:"classification,omitempty"`
	// FilterReason is set when the channel is dropped from export (exclusion, DRM, etc.).
	FilterReason string `json:"filter_reason,omitempty"`
	Excluded     bool   `json:"excluded"`
}

// MatchesExclusion reports whether any compiled regex matches stream URL, provider id, or name.
// Patterns are expected already compiled with (?i) as config does at load.
func (c Channel) MatchesExclusion(regexes []*regexp.Regexp) (matched bool, reason string) {
	haystacks := []string{c.StreamURL, string(c.Provider), c.ID, c.Name, c.NormalizedID}
	for _, re := range regexes {
		if re == nil {
			continue
		}
		for _, h := range haystacks {
			if h != "" && re.MatchString(h) {
				return true, fmt.Sprintf("exclusion %q matched", re.String())
			}
		}
	}
	return false, ""
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

// Normalize fills NormalizedID and cleans Name/Group for storage (quote-safe text).
func (c *Channel) Normalize() {
	if c == nil {
		return
	}
	c.ID = strings.TrimSpace(c.ID)
	c.NormalizedID = NormalizeID(c.ID)
	c.Name = strings.TrimSpace(format.StripQuotes(c.Name))
	c.Group = strings.TrimSpace(format.StripQuotes(c.Group))
}

// ForExport returns channels that belong in M3U/XMLTV playlists.
// Excluded channels and rows missing a normalized id or stream URL are omitted.
func ForExport(channels []Channel) []Channel {
	out := make([]Channel, 0, len(channels))
	for _, ch := range channels {
		if ch.Excluded || ch.NormalizedID == "" || ch.StreamURL == "" {
			continue
		}
		out = append(out, ch)
	}
	return out
}

// MarkExclusions marks channels that match any provider exclusion regex.
// Excluded channels stay in the slice with Excluded/FilterReason set so the UI can explain drops.
func MarkExclusions(channels []Channel, regexes []*regexp.Regexp) []Channel {
	if len(regexes) == 0 {
		return channels
	}
	out := make([]Channel, len(channels))
	copy(out, channels)
	for i := range out {
		if ok, reason := out[i].MatchesExclusion(regexes); ok {
			out[i].Excluded = true
			out[i].FilterReason = reason
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
