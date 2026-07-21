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

// Reader fetches and decodes one provider's lineup. Fetch performs transport
// only and returns the exact provider-native response. Parse decodes those bytes
// without network access, so refresh and cache restore share one decoder.
type Reader interface {
	Fetch(ctx context.Context) ([]byte, error)
	Parse(raw []byte) ([]model.Channel, []model.Programme, error)
}

// SkipMalformed increments a skip counter for a bad upstream record.
// Readers must not panic on malformed data — call this and continue.
func SkipMalformed(skipped *int) {
	if skipped != nil {
		*skipped++
	}
}
