// Package distrotv wires DistroTV into the shared published-pair implementation.
package distrotv

import (
	"time"

	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider/published"
)

var source = published.Source{
	ID:      model.ProviderDistroTV,
	M3UURL:  "https://raw.githubusercontent.com/vraomoturi/DistroTV/master/distrotv.m3u",
	EPGURL:  "https://raw.githubusercontent.com/vraomoturi/DistroTV/master/distrotv.xml.gz",
	EPGGzip: true,
}

// DefaultSettings returns DistroTV's built-in settings.
func DefaultSettings() model.ProviderSettings {
	settings := model.DefaultSettings()
	settings.ID = model.ProviderDistroTV
	settings.Label = "DistroTV"
	settings.SynthesizeChannelNumbers = 6000
	settings.MinChannels = 50
	settings.RefreshInterval = 6 * time.Hour
	return settings
}

// New constructs the DistroTV provider.
func New(settings model.ProviderSettings, client *httpx.Client) *published.Client {
	return published.New(source, settings, client)
}
