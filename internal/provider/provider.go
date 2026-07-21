// Package provider defines the Reader port, the runtime Feed that holds a
// provider's last-good lineup, and the registry of feeds.
//
// Provider implementations are code: each provider package (e.g. internal/provider/lg)
// exposes a New constructor returning a concrete type that satisfies Reader. The
// bootstrap wires those into a map[model.ProviderID]Reader; YAML only overlays settings.
package provider

import (
	"context"

	"github.com/j27-aurum/gofast/internal/model"
)

// Reader fetches and decodes one provider's lineup. Fetch is the transport
// (GET the upstream, archive the raw bytes, then Parse them); Parse decodes raw
// bytes with no network, so the same bytes can be re-loaded from the cache on
// boot exactly as if freshly fetched.
type Reader interface {
	Fetch(ctx context.Context) ([]model.Channel, []model.Programme, error)
	Parse(raw []byte) ([]model.Channel, []model.Programme, error)
}

// RawWriter archives a provider's raw upstream response (a debug backup). It is
// satisfied by *cache.Cache; adapters call it without importing the cache, so
// the cache stays the sole owner of disk.
type RawWriter interface {
	WriteRaw(id model.ProviderID, raw []byte) error
}

// SkipMalformed increments a skip counter for a bad upstream record.
// Readers must not panic on malformed data — call this and continue.
func SkipMalformed(skipped *int) {
	if skipped != nil {
		*skipped++
	}
}
