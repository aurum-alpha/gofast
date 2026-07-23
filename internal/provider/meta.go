package provider

import (
	"time"

	"github.com/j27-aurum/gofast/internal/model"
)

// Meta is the small, persisted per-provider record (cache meta.json). It holds
// only what cannot be reproduced by re-parsing the current raw snapshot: fetch
// time and historical synthetic channel numbers (including IDs no longer
// present). Classifications live in the channel-attr store (see
// internal/channelattr); Classifications here is read-only for one-time seed
// from older generations.
type Meta struct {
	FetchedAt               time.Time                       `json:"fetched_at"`
	Classifications         map[string]model.Classification `json:"classifications,omitempty"`
	SyntheticChannelNumbers ChannelNumberAssignments        `json:"synthetic_channel_numbers,omitempty"`
}

// MetaOf extracts the persistable Meta from a runtime Lineup.
// Classifications are not written — they persist via channelattr KindClassification.
func MetaOf(l Lineup) Meta {
	return Meta{
		FetchedAt:               l.FetchedAt,
		SyntheticChannelNumbers: l.SyntheticChannelNumbers.Clone(),
	}
}
