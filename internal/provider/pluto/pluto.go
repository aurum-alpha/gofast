// Package pluto wires Pluto TV US into the shared MJH implementation.
package pluto

import (
	"time"

	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider/mjh"
)

var source = mjh.Source{
	ID:          model.ProviderPluto,
	Directory:   "PlutoTV",
	DefaultSlug: "plu-{id}.m3u8",
}

// DefaultSettings returns Pluto's built-in settings.
func DefaultSettings() model.ProviderSettings {
	settings := model.DefaultSettings()
	settings.ID = model.ProviderPluto
	settings.Label = "PlutoTV"
	settings.Region = "us"
	settings.ChannelNumberOffset = 2000
	settings.MinChannels = 50
	settings.RefreshInterval = 6 * time.Hour
	settings.SlugTemplate = source.DefaultSlug
	return settings
}

// New constructs the Pluto provider.
func New(settings model.ProviderSettings, client *httpx.Client) *mjh.Client {
	return mjh.New(source, settings, client)
}
