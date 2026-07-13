// Package model holds the shared channel/programme types used across adapters,
// classifier, emitters, and the UI. Domain types may carry self-contained
// business logic (normalization, exclusions) that only needs the model itself.
package model

import "time"

// Classification is the stream classifier bucket.
type Classification string

const (
	ClassNative Classification = "NATIVE"
	ClassBeacon Classification = "BEACON"
	ClassDRM    Classification = "DRM"
)

// Channel is a lineup entry after provider fetch (+ optional classify).
type Channel struct {
	Provider  string // config key, e.g. "lg", "pluto"
	ID        string // upstream id before Normalize
	Name      string // clean display name (tvg-name)
	Group     string // upstream group / genre
	Number    int    // upstream channel number; 0 if none / synthesize later
	StreamURL string
	LogoURL   string

	// NormalizedID is NormalizeID(ID) — same value for M3U tvg-id, XMLTV channel id,
	// and programme channel references.
	NormalizedID string

	Classification Classification
	// FilterReason is set when the channel is dropped from export (exclusion, DRM, etc.).
	FilterReason string
	Excluded     bool
}

// Programme is a guide entry keyed by NormalizedID of its channel.
type Programme struct {
	ChannelID string // normalized channel id
	Title     string
	Desc      string
	Start     time.Time
	Stop      time.Time
}
