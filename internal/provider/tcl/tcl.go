// Package tcl wires TCL TV+ into the shared published-pair implementation.
package tcl

import (
	"time"

	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider/published"
)

var source = published.Source{
	ID:     model.ProviderTCL,
	M3UURL: "https://raw.githubusercontent.com/BuddyChewChew/tcl-playlist-generator/refs/heads/main/tcl.m3u8",
	EPGURL: "https://raw.githubusercontent.com/BuddyChewChew/tcl-playlist-generator/refs/heads/main/tcl_epg.xml",
}

// DefaultSettings returns TCL's built-in settings.
func DefaultSettings() model.ProviderSettings {
	settings := model.DefaultSettings()
	settings.ID = model.ProviderTCL
	settings.Label = "TCL"
	settings.SynthesizeChannelNumbers = 10000
	settings.MinChannels = 200
	settings.RefreshInterval = 6 * time.Hour
	settings.ExpectedGuideHorizon = 48 * time.Hour
	return settings
}

// New constructs the TCL provider.
func New(settings model.ProviderSettings, client *httpx.Client) *published.Client {
	return published.New(source, settings, client)
}
