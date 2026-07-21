// Package format builds playlist and guide presentation strings from raw fields.
// These are output-shaping helpers (M3U / XMLTV text), not domain mutations.
package format

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// CombinedID namespaces a channel id for a combined (all-provider) document,
// where bare normalized ids from different providers could collide. The scheme
// is "{provider}.{normalizedID}"; per-provider documents keep the bare id.
func CombinedID(provider, normalizedID string) string {
	return provider + "." + normalizedID
}

// FormatDisplayName is the M3U comma-name / XMLTV display-name: "{name} · {label}".
// It preserves punctuation; the target encoder applies context-specific escaping.
func FormatDisplayName(name, label string) string {
	name = strings.TrimSpace(name)
	label = strings.TrimSpace(label)
	if label == "" {
		return name
	}
	if name == "" {
		return label
	}
	return name + " · " + label
}

// FormatGroupTitle is M3U group-title: "{label}: {group}".
func FormatGroupTitle(label, group string) string {
	label = strings.TrimSpace(label)
	group = strings.TrimSpace(group)
	if label == "" {
		return group
	}
	if group == "" {
		return label
	}
	return label + ": " + group
}

// M3UAttribute makes a value safe inside a double-quoted M3U attribute.
// M3U has no universal escaping convention, so double quotes are removed and
// controls are collapsed to spaces while apostrophes and Unicode are retained.
func M3UAttribute(value string) string {
	return sanitizeM3U(value, true)
}

// M3UText makes human-readable M3U text safe for a single physical line.
func M3UText(value string) string {
	return sanitizeM3U(value, false)
}

// ValidM3ULine reports whether value can be emitted unchanged as one M3U line.
// Playback URLs use this strict check because mutating a URL could retarget it.
func ValidM3ULine(value string) bool {
	if value == "" || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if lineBreakingRune(r) {
			return false
		}
	}
	return true
}

func sanitizeM3U(value string, stripDoubleQuotes bool) string {
	value = strings.ToValidUTF8(value, "\uFFFD")
	var b strings.Builder
	b.Grow(len(value))
	pendingSpace := false
	for _, r := range value {
		if lineBreakingRune(r) {
			pendingSpace = b.Len() > 0
			continue
		}
		if stripDoubleQuotes && r == '"' {
			continue
		}
		if pendingSpace && !unicode.IsSpace(r) {
			b.WriteByte(' ')
		}
		pendingSpace = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func lineBreakingRune(r rune) bool {
	return unicode.IsControl(r) || r == '\u2028' || r == '\u2029'
}
