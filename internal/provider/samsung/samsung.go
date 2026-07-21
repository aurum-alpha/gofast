// Package samsung wires Samsung TV Plus US into the shared MJH implementation.
package samsung

import (
	"time"

	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider/mjh"
)

var source = mjh.Source{
	ID:        model.ProviderSamsung,
	Directory: "SamsungTVPlus",
}

// DefaultSettings returns Samsung's built-in settings.
func DefaultSettings() model.ProviderSettings {
	settings := model.DefaultSettings()
	settings.ID = model.ProviderSamsung
	settings.Label = "SamsungTVPlus"
	settings.Region = "us"
	settings.ChannelNumberOffset = 3000
	settings.MinChannels = 50
	settings.RefreshInterval = 6 * time.Hour
	return settings
}

// New constructs the Samsung provider.
func New(settings model.ProviderSettings, client *httpx.Client) *mjh.Client {
	return mjh.New(source, settings, client)
}
