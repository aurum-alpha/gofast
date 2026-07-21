// Package localnow wires LocalNow into the shared published-pair implementation.
package localnow

import (
	"time"

	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider/published"
)

const browserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

var source = published.Source{
	ID:          model.ProviderLocalNow,
	M3UURL:      "https://www.apsattv.com/localnow.m3u",
	EPGURL:      "https://raw.githubusercontent.com/BuddyChewChew/localnow-playlist-generator/refs/heads/main/epg.xml",
	GroupPrefix: "LocalNow🇺🇸:",
}

// DefaultSettings returns LocalNow's built-in settings.
func DefaultSettings() model.ProviderSettings {
	settings := model.DefaultSettings()
	settings.ID = model.ProviderLocalNow
	settings.Label = "LocalNow"
	settings.SynthesizeChannelNumbers = 7000
	settings.MinChannels = 10
	settings.RefreshInterval = 6 * time.Hour
	settings.UserAgent = browserUserAgent
	return settings
}

// New constructs the LocalNow provider.
func New(settings model.ProviderSettings, client *httpx.Client) *published.Client {
	return published.New(source, settings, client)
}
