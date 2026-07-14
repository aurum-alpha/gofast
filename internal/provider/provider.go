// Package provider defines the Reader interface and a registry of readers.
//
// Provider implementations are code: each provider package (e.g. internal/provider/lg)
// exposes a New constructor returning a concrete type that satisfies Reader. The
// bootstrap wires those constructors into a map[id]Reader; YAML only overlays settings.
package provider

import (
	"context"
	"log/slog"
	"sort"

	"github.com/j27-aurum/gofast/internal/model"
)

// Reader fetches a normalized lineup for one configured provider id.
type Reader interface {
	ID() string
	Fetch(ctx context.Context) ([]model.Channel, []model.Programme, error)
}

// Result is the outcome of fetching one enabled provider.
type Result struct {
	ID         string
	Channels   []model.Channel
	Programmes []model.Programme
	Skipped    int // malformed records skipped inside the reader (no panic)
	Err        error
}

// Registry pairs the readers wired in code with the effective settings per id.
// readers holds only enabled providers; settings covers every known id (so the
// API/logs can show disabled ones too).
type Registry struct {
	readers  map[string]Reader
	settings map[string]model.ProviderSettings
	ids      []string // all known ids, sorted
}

// NewRegistry wires already-built readers with their effective settings.
func NewRegistry(readers map[string]Reader, settings map[string]model.ProviderSettings) *Registry {
	ids := make([]string, 0, len(settings))
	for id := range settings {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return &Registry{readers: readers, settings: settings, ids: ids}
}

// FetchAll runs Fetch on each enabled reader and applies Channel.Normalize + exclusions.
// A single provider error does not abort the others; it is recorded on Result.Err.
func (r *Registry) FetchAll(ctx context.Context) []Result {
	out := make([]Result, 0, len(r.readers))
	for _, id := range r.ids {
		reader, ok := r.readers[id]
		if !ok {
			continue // disabled / no implementation
		}
		s := r.settings[id]
		res := Result{ID: id}
		chs, progs, err := reader.Fetch(ctx)
		if err != nil {
			res.Err = err
			out = append(out, res)
			continue
		}
		for i := range chs {
			chs[i].Provider = id
			chs[i].Normalize()
			chs[i].ApplyChannelNumberOffset(s.ChannelNumberOffset)
		}
		chs = model.MarkExclusions(chs, s.ExclusionRegexes)
		normByRaw := make(map[string]string, len(chs))
		for _, ch := range chs {
			normByRaw[ch.ID] = ch.NormalizedID
		}
		for i := range progs {
			raw := progs[i].ChannelID
			if n, ok := normByRaw[raw]; ok {
				progs[i].ChannelID = n
			} else {
				progs[i].ChannelID = model.NormalizeID(raw)
			}
		}
		res.Channels = chs
		res.Programmes = progs
		out = append(out, res)
	}
	return out
}

// IDs returns the enabled (wired) provider ids in sorted order.
func (r *Registry) IDs() []string {
	out := make([]string, 0, len(r.readers))
	for _, id := range r.ids {
		if _, ok := r.readers[id]; ok {
			out = append(out, id)
		}
	}
	return out
}

// LogLoaded logs one line per known provider with its effective settings.
func (r *Registry) LogLoaded() {
	for _, p := range r.Providers().Providers {
		slog.Info("provider",
			"id", p.ID,
			"enabled", p.IsEnabled(),
			"label", p.Label,
			"channel_number_offset", p.ChannelNumberOffset,
			"synthesize_channel_numbers", p.SynthesizeChannelNumbers,
			"min_channels", p.MinChannels,
			"refresh_interval", p.RefreshInterval.String(),
			"exclusions", len(p.Exclusions),
			"slug_template", p.SlugTemplate,
			"region", p.Region,
		)
	}
}

// Providers returns all known providers (enabled + disabled) sorted by id, with
// their effective settings — the source for GET /api/providers and startup logs.
func (r *Registry) Providers() model.ProviderList {
	return model.ListProviders(r.settings)
}

// Settings returns the effective settings for id (zero value if unknown).
func (r *Registry) Settings(id string) model.ProviderSettings {
	return r.settings[id]
}

// SkipMalformed increments a skip counter for a bad upstream record.
// Readers must not panic on malformed data — call this and continue.
func SkipMalformed(skipped *int) {
	if skipped != nil {
		*skipped++
	}
}
