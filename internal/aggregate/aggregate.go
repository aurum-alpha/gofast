// Package aggregate builds the combined (all-provider) playlist.m3u and epg.xml
// from the registry feeds and writes them via the cache. It is the sole writer
// of the aggregate files, driven by a coalesced signal — separate from refresh's
// per-provider scheduling.
package aggregate

import (
	"context"
	"log/slog"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/m3u"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/xmltv"
)

// Aggregator regenerates the combined artifacts on demand.
type Aggregator struct {
	reg   *provider.Registry
	cache *cache.Cache
	dirty chan struct{}
}

// New returns an Aggregator over the registry feeds.
func New(reg *provider.Registry, cc *cache.Cache) *Aggregator {
	return &Aggregator{reg: reg, cache: cc, dirty: make(chan struct{}, 1)}
}

// Notify requests a rebuild. It is non-blocking and coalesces bursts: many
// notifies between rebuilds collapse into a single rebuild.
func (a *Aggregator) Notify() {
	select {
	case a.dirty <- struct{}{}:
	default:
	}
}

// Rebuild regenerates playlist.m3u + epg.xml from the current feeds (namespaced
// ids) and writes them atomically via the cache.
func (a *Aggregator) Rebuild() error {
	feeds := a.reg.Feeds()
	msrcs := make([]m3u.Source, 0, len(feeds))
	xsrcs := make([]xmltv.Source, 0, len(feeds))
	for _, f := range feeds {
		lin := f.Lineup()
		id := f.ID()
		msrcs = append(msrcs, m3u.Source{Provider: id, Label: f.Label(), Channels: lin.Channels})
		xsrcs = append(xsrcs, xmltv.Source{Provider: id, Label: f.Label(), Channels: lin.Channels, Programmes: lin.Programmes})
	}

	m3uData, err := m3u.MarshalAll(msrcs, m3u.Options{NamespaceIDs: true})
	if err != nil {
		return err
	}
	xmlData, err := xmltv.MarshalAll(xsrcs, xmltv.Options{NamespaceIDs: true})
	if err != nil {
		return err
	}
	return a.cache.WriteAggregate(cache.M3U(m3uData), cache.XMLTV(xmlData))
}

// Run rebuilds on each coalesced signal until ctx is cancelled.
func (a *Aggregator) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.dirty:
			if err := a.Rebuild(); err != nil {
				slog.Warn("aggregate rebuild failed", "err", err)
			}
		}
	}
}
