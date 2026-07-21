// Package xumo wires Xumo Play into the shared published-pair implementation.
package xumo

import (
	"time"

	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider/published"
)

var source = published.Source{
	ID:      model.ProviderXumo,
	M3UURL:  "https://raw.githubusercontent.com/BuddyChewChew/xumo-playlist-generator/main/playlists/xumo_playlist.m3u",
	EPGURL:  "https://raw.githubusercontent.com/BuddyChewChew/xumo-playlist-generator/main/playlists/xumo_epg.xml.gz",
	EPGGzip: true,
}

// DefaultSettings returns Xumo's built-in settings.
func DefaultSettings() model.ProviderSettings {
	settings := model.DefaultSettings()
	settings.ID = model.ProviderXumo
	settings.Label = "Xumo"
	settings.SynthesizeChannelNumbers = 5000
	settings.MinChannels = 100
	settings.RefreshInterval = 6 * time.Hour
	return settings
}

// New constructs the Xumo provider.
func New(settings model.ProviderSettings, client *httpx.Client) *published.Client {
	return published.New(source, settings, client)
}
