package provider

import (
	"time"

	"github.com/j27-aurum/gofast/internal/model"
)

// Meta is the small, persisted per-provider record (cache meta.json). It holds
// only what cannot be reproduced by re-parsing the current raw snapshot: fetch
// time, network-derived classifications, and historical synthetic channel
// numbers (including IDs no longer present).
type Meta struct {
	FetchedAt               time.Time                       `json:"fetched_at"`
	Classifications         map[string]model.Classification `json:"classifications,omitempty"`
	SyntheticChannelNumbers ChannelNumberAssignments        `json:"synthetic_channel_numbers,omitempty"`
}

// MetaOf extracts the persistable Meta from a runtime Lineup.
func MetaOf(l Lineup) Meta {
	m := Meta{
		FetchedAt:               l.FetchedAt,
		SyntheticChannelNumbers: l.SyntheticChannelNumbers.Clone(),
	}
	for _, ch := range l.Channels {
		if ch.Classification == "" {
			continue
		}
		if m.Classifications == nil {
			m.Classifications = make(map[string]model.Classification, len(l.Channels))
		}
		m.Classifications[ch.NormalizedID] = ch.Classification
	}
	return m
}
