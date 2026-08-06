// Package stirr fetches STIRR free AVOD FAST channels from stirr.com.
// Catalog StreamURLs are opaque; FASTProxy resolves a fresh HLS URL at tune-in
// via POST /api/v2/videos/{id}/playable (STIRR_RESOLVE).
package stirr

import (
	"time"

	"github.com/j27-aurum/gofast/internal/model"
)

const (
	// DefaultChannelsURL is the live channel list (content_type=4 = live).
	DefaultChannelsURL = "https://stirr.com/api/videos/list/?categories=all_categories&content_type=4&no_limit=true"
	// DefaultEPGURL is STIRR's bulk guide endpoint.
	DefaultEPGURL = "https://stirr.com/api/epg"
	// DefaultPlayableURLTemplate builds the tune-in POST URL (%s = videoid).
	DefaultPlayableURLTemplate = "https://stirr.com/api/v2/videos/%s/playable"

	BrowserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36"

	RawList = "list.json"
	RawEPG  = "epg.json"
	RawDead = "dead.json" // JSON string array of videoids with Aniview CON
)

// DefaultSettings returns STIRR's built-in settings.
// Disabled by default: requires FASTProxy for STIRR_RESOLVE tune-in.
func DefaultSettings() model.ProviderSettings {
	settings := model.DefaultSettings()
	settings.ID = model.ProviderSTIRR
	settings.Label = "STIRR"
	enabled := false
	settings.Enabled = &enabled
	settings.SynthesizeChannelNumbers = 11000
	settings.MinChannels = 50
	settings.RefreshInterval = 6 * time.Hour
	settings.ExpectedGuideHorizon = 24 * time.Hour
	settings.UserAgent = BrowserUA
	return settings
}
