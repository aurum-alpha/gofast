// Package distrotv fetches DistroTV from Distro's jsrdn live feed (not the
// stale published DAI M3U). Catalog StreamURLs are opaque; FASTProxy resolves
// a fresh HLS URL at tune-in.
package distrotv

import (
	"time"

	"github.com/j27-aurum/gofast/internal/model"
)

// DefaultSettings returns DistroTV's built-in settings.
// Distro is disabled by default: the live lineup is small and requires proxy.
func DefaultSettings() model.ProviderSettings {
	settings := model.DefaultSettings()
	settings.ID = model.ProviderDistroTV
	settings.Label = "DistroTV"
	enabled := false
	settings.Enabled = &enabled
	settings.Region = DefaultGeo
	settings.SynthesizeChannelNumbers = 6000
	settings.MinChannels = 1
	settings.RefreshInterval = 6 * time.Hour
	settings.ExpectedGuideHorizon = 24 * time.Hour
	settings.UserAgent = AndroidUA
	return settings
}
