package stirr

import (
	"strings"
)

const (
	// OpaqueSchemePrefix is the catalog StreamURL prefix for STIRR channels.
	// FASTProxy resolves a fresh HLS URL via POST /playable at tune-in.
	OpaqueSchemePrefix = "stirr://channel/"
)

// OpaqueStreamURL builds the catalog StreamURL for a STIRR videoid.
func OpaqueStreamURL(id string) string {
	return OpaqueSchemePrefix + strings.TrimSpace(id)
}

// ParseOpaque extracts the videoid from a STIRR opaque StreamURL.
func ParseOpaque(streamURL string) (channelID string, ok bool) {
	s := strings.TrimSpace(streamURL)
	if !strings.HasPrefix(s, OpaqueSchemePrefix) {
		return "", false
	}
	id := strings.TrimSpace(strings.TrimPrefix(s, OpaqueSchemePrefix))
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}
