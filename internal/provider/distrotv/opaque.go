package distrotv

import (
	"strings"
)

const (
	// OpaqueSchemePrefix is the catalog StreamURL prefix for Distro channels.
	// FASTProxy resolves a fresh HLS URL from the jsrdn feed at tune-in.
	OpaqueSchemePrefix = "distro://channel/"
)

// OpaqueStreamURL builds the catalog StreamURL for a Distro channel.
// id must already be the stable catalog id (e.g. QQ_95226).
func OpaqueStreamURL(id string) string {
	return OpaqueSchemePrefix + strings.TrimSpace(id)
}

// ParseOpaque extracts the catalog channel id from a Distro opaque StreamURL.
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

// SplitChannelID splits "{geo}_{rawID}" into parts. geo defaults to DefaultGeo
// when the id has no underscore separator.
func SplitChannelID(channelID, defaultGeo string) (geo, rawID string) {
	channelID = strings.TrimSpace(channelID)
	if defaultGeo == "" {
		defaultGeo = DefaultGeo
	}
	geo = NormalizeGeo(defaultGeo)
	rawID = channelID
	if i := strings.IndexByte(channelID, '_'); i > 0 {
		geo = NormalizeGeo(channelID[:i])
		rawID = channelID[i+1:]
	}
	return geo, rawID
}

// JoinChannelID builds a stable catalog id that survives NormalizeID (colons
// would be stripped).
func JoinChannelID(geo, rawID string) string {
	return NormalizeGeo(geo) + "_" + strings.TrimSpace(rawID)
}
