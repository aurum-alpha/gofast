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

// LegacyRaw is the synthetic name used when loading the pre-multipart raw file.
const LegacyRaw = "legacy"

// Raw is one provider's exact upstream payloads, keyed by descriptive filename.
// A refresh may require multiple coordinated responses (for example MJH channel
// metadata plus XMLTV); the cache publishes all files in one generation.
type Raw map[string][]byte

// Reader fetches and decodes one provider's lineup. Fetch performs transport
// only and returns exact provider-native responses. Parse decodes those bytes
// without network access, so refresh and cache restore share one decoder.
type Reader interface {
	Fetch(ctx context.Context) (Raw, error)
	Parse(raw Raw) ([]model.Channel, []model.Programme, error)
}

// SkipMalformed increments a skip counter for a bad upstream record.
// Readers must not panic on malformed data — call this and continue.
func SkipMalformed(skipped *int) {
	if skipped != nil {
		*skipped++
	}
}
