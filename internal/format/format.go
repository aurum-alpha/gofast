// Package format builds playlist and guide presentation strings from raw fields.
// These are output-shaping helpers (M3U / XMLTV text), not domain mutations.
package format

import "strings"

// StripQuotes removes " and ' from s.
// Needed for hand-built M3U attributes (tvg-name="..."); encoding/xml escapes
// quotes instead of removing them, and there is no stdlib M3U encoder.
func StripQuotes(s string) string {
	s = strings.ReplaceAll(s, `"`, "")
	s = strings.ReplaceAll(s, "'", "")
	return s
}

// FormatDisplayName is the M3U comma-name / XMLTV display-name: "{name} · {label}".
// If label is empty, returns the cleaned name alone.
func FormatDisplayName(name, label string) string {
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

// FormatGroupTitle is M3U group-title: "{label}: {group}".
func FormatGroupTitle(label, group string) string {
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

// CombinedID namespaces a channel id for a combined (all-provider) document,
// where bare normalized ids from different providers could collide. The scheme
// is "{provider}.{normalizedID}"; per-provider documents keep the bare id.
func CombinedID(provider, normalizedID string) string {
	return provider + "." + normalizedID
}
