package provider

import (
	"time"

	"github.com/j27-aurum/gofast/internal/model"
)

// Meta is the small, persisted per-provider record (cache meta.json). It holds
// only what cannot be cheaply reproduced by re-parsing the raw upstream snapshot
// with the current settings: the fetch time (for scheduling) and the expensive,
// network-derived classifications keyed by normalized channel id.
type Meta struct {
	FetchedAt       time.Time                       `json:"fetched_at"`
	Classifications map[string]model.Classification `json:"classifications,omitempty"`
}

// MetaOf extracts the persistable Meta from a runtime Lineup.
func MetaOf(l Lineup) Meta {
	m := Meta{FetchedAt: l.FetchedAt}
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
