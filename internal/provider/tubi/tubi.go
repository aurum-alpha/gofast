// Package tubi wires Tubi into the shared published-pair implementation.
package tubi

import (
	"time"

	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider/published"
)

var source = published.Source{
	ID:     model.ProviderTubi,
	M3UURL: "https://raw.githubusercontent.com/BuddyChewChew/app-m3u-generator/main/playlists/tubi_all.m3u",
	EPGURL: "https://raw.githubusercontent.com/BuddyChewChew/app-m3u-generator/main/playlists/tubi_epg.xml",
}

// DefaultSettings returns Tubi's built-in settings.
func DefaultSettings() model.ProviderSettings {
	settings := model.DefaultSettings()
	settings.ID = model.ProviderTubi
	settings.Label = "Tubi"
	settings.SynthesizeChannelNumbers = 9000
	settings.MinChannels = 100
	settings.RefreshInterval = 6 * time.Hour
	settings.ExpectedGuideHorizon = 48 * time.Hour
	return settings
}

// New constructs the Tubi provider.
func New(settings model.ProviderSettings, client *httpx.Client) *published.Client {
	return published.New(source, settings, client)
}
