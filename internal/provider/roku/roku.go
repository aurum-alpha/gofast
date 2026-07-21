// Package roku wires the Roku Channel into the shared MJH implementation.
package roku

import (
	"time"

	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider/mjh"
)

var source = mjh.Source{
	ID:          model.ProviderRoku,
	Directory:   "Roku",
	Regionless:  true,
	DefaultSlug: "rok-{id}.m3u8",
}

// DefaultSettings returns Roku's built-in settings.
func DefaultSettings() model.ProviderSettings {
	settings := model.DefaultSettings()
	settings.ID = model.ProviderRoku
	settings.Label = "Roku"
	settings.ChannelNumberOffset = 4000
	settings.MinChannels = 50
	settings.RefreshInterval = 6 * time.Hour
	settings.SlugTemplate = source.DefaultSlug
	return settings
}

// New constructs the Roku provider.
func New(settings model.ProviderSettings, client *httpx.Client) *mjh.Client {
	return mjh.New(source, settings, client)
}
