// Package provider defines the Reader interface and a config-driven registry.
package provider

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/model"
)

// Reader fetches a normalized lineup for one configured provider id.
type Reader interface {
	ID() string
	Fetch(ctx context.Context) ([]model.Channel, []model.Programme, error)
}

// Factory constructs a Reader from a model.Provider config block.
type Factory func(id string, cfg model.Provider) (Reader, error)

// Result is the outcome of fetching one enabled provider.
type Result struct {
	ID         string
	Channels   []model.Channel
	Programmes []model.Programme
	Skipped    int // malformed records skipped inside the adapter (no panic)
	Err        error
}

// Registry holds enabled readers built from config + registered factories.
type Registry struct {
	items []entry
}

type entry struct {
	id  string
	cfg model.Provider
	r   Reader
}

// NewRegistry builds a registry for enabled providers that have a registered factory.
// Unknown ids (no factory yet) are skipped with a warning — adapters land later.
// Disabled providers are omitted.
func NewRegistry(cfg *config.Config, factories map[string]Factory) (*Registry, error) {
	ids := make([]string, 0, len(cfg.Providers))
	for id := range cfg.Providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	r := &Registry{}
	for _, id := range ids {
		pcfg := cfg.Providers[id]
		if !pcfg.IsEnabled() {
			slog.Info("provider disabled", "id", id)
			continue
		}
		factory, ok := factories[id]
		if !ok {
			slog.Warn("provider has no adapter registered yet", "id", id)
			continue
		}
		reader, err := factory(id, pcfg)
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", id, err)
		}
		if reader == nil {
			return nil, fmt.Errorf("provider %q: factory returned nil", id)
		}
		r.items = append(r.items, entry{id: id, cfg: pcfg, r: reader})
	}
	return r, nil
}

// IDs returns enabled, registered provider ids in sorted order.
func (r *Registry) IDs() []string {
	out := make([]string, len(r.items))
	for i, e := range r.items {
		out[i] = e.id
	}
	return out
}

// FetchAll runs Fetch on each registered reader and applies Channel.Normalize + exclusions.
// A single provider error does not abort the others; it is recorded on Result.Err.
func (r *Registry) FetchAll(ctx context.Context) []Result {
	out := make([]Result, 0, len(r.items))
	for _, e := range r.items {
		res := Result{ID: e.id}
		chs, progs, err := e.r.Fetch(ctx)
		if err != nil {
			res.Err = err
			out = append(out, res)
			continue
		}
		for i := range chs {
			chs[i].Provider = e.id
			chs[i].Normalize()
			chs[i].ApplyChnoOffset(e.cfg.ChnoOffset)
		}
		chs = model.MarkExclusions(chs, e.cfg.ExclusionRegexes)
		res.Channels = chs
		res.Programmes = progs
		out = append(out, res)
	}
	return out
}

// SkipMalformed increments a skip counter for a bad upstream record.
// Adapters must not panic on malformed data — call this and continue.
func SkipMalformed(skipped *int) {
	if skipped != nil {
		*skipped++
	}
}
