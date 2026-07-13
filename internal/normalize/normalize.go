// Package normalize applies stable id/name/number transforms shared by all providers.
package normalize

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/j27-aurum/gofast/internal/model"
)

// allowedID keeps [A-Za-z0-9._-] after whitespace→_ and quote/control stripping.
var allowedID = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// ID normalizes a channel id for M3U tvg-id, XMLTV <channel id>, and programme channel.
// Deterministic: same input always yields the same output.
func ID(raw string) string {
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
	// Collapse runs of underscores from whitespace sequences.
	for strings.Contains(s, "__") {
		s = strings.ReplaceAll(s, "__", "_")
	}
	return strings.Trim(s, "_")
}

// StripQuotes removes double quotes (and replaces leftovers) for M3U attribute safety.
func StripQuotes(s string) string {
	s = strings.ReplaceAll(s, `"`, "")
	s = strings.ReplaceAll(s, "'", "")
	return s
}

// DisplayName is the M3U display name: "{name} · {label}".
// If label is empty, returns the stripped name alone.
func DisplayName(name, label string) string {
	name = strings.TrimSpace(StripQuotes(name))
	label = strings.TrimSpace(StripQuotes(label))
	if label == "" {
		return name
	}
	if name == "" {
		return label
	}
	return name + " · " + label
}

// GroupTitle is M3U group-title: "{label}: {group}".
func GroupTitle(label, group string) string {
	label = strings.TrimSpace(StripQuotes(label))
	group = strings.TrimSpace(StripQuotes(group))
	if label == "" {
		return group
	}
	if group == "" {
		return label
	}
	return label + ": " + group
}

// ApplyChnoOffset adds a per-provider offset to an upstream channel number.
// native <= 0 means "no upstream number" — returns 0 (synthesize path handles it).
func ApplyChnoOffset(native, offset int) int {
	if native <= 0 {
		return 0
	}
	return native + offset
}

// ApplyChannel fills NormalizedID and applies quote stripping to Name/Group.
func ApplyChannel(ch *model.Channel) {
	if ch == nil {
		return
	}
	ch.ID = strings.TrimSpace(ch.ID)
	ch.NormalizedID = ID(ch.ID)
	ch.Name = strings.TrimSpace(StripQuotes(ch.Name))
	ch.Group = strings.TrimSpace(StripQuotes(ch.Group))
}

// MatchExclusion reports whether any compiled regex matches stream URL, provider id, or name.
// Patterns are expected already compiled with (?i) as config does at load.
func MatchExclusion(ch model.Channel, regexes []*regexp.Regexp) (matched bool, reason string) {
	haystacks := []string{ch.StreamURL, ch.Provider, ch.ID, ch.Name, ch.NormalizedID}
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

// ApplyExclusions marks channels that match any provider exclusion regex.
// Excluded channels stay in the slice with Excluded/FilterReason set so the UI can explain drops.
func ApplyExclusions(channels []model.Channel, regexes []*regexp.Regexp) []model.Channel {
	if len(regexes) == 0 {
		return channels
	}
	out := make([]model.Channel, len(channels))
	copy(out, channels)
	for i := range out {
		if ok, reason := MatchExclusion(out[i], regexes); ok {
			out[i].Excluded = true
			out[i].FilterReason = reason
		}
	}
	return out
}
