// Package plex wires Plex Free TV into the shared MJH implementation.
//
// Plex metadata is tagged-region shaped: channels live at the top level and
// each channel lists membership in a regions[] array. Region blocks supply
// headers only (no nested channels map). EPG remains {region}.xml.gz.
package plex

import (
	"time"

	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider/mjh"
)

var source = mjh.Source{
	ID:            model.ProviderPlex,
	Directory:     "Plex",
	TaggedRegions: true,
	DefaultSlug:   "plex-{id}.m3u8",
}

// DefaultSettings returns Plex's built-in settings.
func DefaultSettings() model.ProviderSettings {
	settings := model.DefaultSettings()
	settings.ID = model.ProviderPlex
	settings.Label = "Plex"
	settings.Region = "us"
	settings.ChannelNumberOffset = 8000
	settings.MinChannels = 50
	settings.RefreshInterval = 6 * time.Hour
	settings.ExpectedGuideHorizon = 48 * time.Hour
	settings.SlugTemplate = source.DefaultSlug
	return settings
}

// New constructs the Plex provider.
func New(settings model.ProviderSettings, client *httpx.Client) *mjh.Client {
	return mjh.New(source, settings, client)
}
