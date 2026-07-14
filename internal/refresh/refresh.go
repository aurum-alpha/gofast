// Package refresh runs provider FetchAll, emits playlists, and updates snapshots.
package refresh

import (
	"bytes"
	"context"
	"log/slog"
	"time"

	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/emit"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/snapshot"
)

// Once fetches all registered providers and publishes gated snapshots.
func Once(ctx context.Context, reg *provider.Registry, cfg *config.Config, store *snapshot.Store) {
	if reg == nil || cfg == nil || store == nil {
		return
	}
	for _, res := range reg.FetchAll(ctx) {
		publish(cfg, store, res)
	}
}

// Loop periodically calls Once until ctx is cancelled.
// Caller should invoke Once once before starting Loop if a warm snapshot is required at boot.
// Thin vertical-slice behavior: in-memory only, min_channels + programme gates,
// keep last-good on failure. Full jitter/disk LKG lands in J27-18.
func Loop(ctx context.Context, reg *provider.Registry, cfg *config.Config, store *snapshot.Store) {
	if reg == nil || cfg == nil || store == nil {
		return
	}
	interval := refreshInterval(cfg)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			Once(ctx, reg, cfg, store)
		}
	}
}

func refreshInterval(cfg *config.Config) time.Duration {
	d := 6 * time.Hour
	for _, p := range cfg.Providers {
		if p.RefreshInterval > 0 && p.RefreshInterval < d {
			d = p.RefreshInterval
		}
	}
	if d < time.Minute {
		return time.Minute
	}
	return d
}

func publish(cfg *config.Config, store *snapshot.Store, res provider.Result) {
	pcfg := cfg.Providers[res.ID]
	label := pcfg.Label
	if label == "" {
		label = res.ID
	}

	if res.Err != nil {
		slog.Warn("refresh failed; keeping last-good", "provider", res.ID, "err", res.Err)
		return
	}

	exported := 0
	for _, ch := range res.Channels {
		if !ch.Excluded && ch.NormalizedID != "" && ch.StreamURL != "" {
			exported++
		}
	}
	minCh := pcfg.MinChannels
	if minCh <= 0 {
		minCh = 1
	}
	if exported < minCh {
		slog.Warn("refresh below min_channels; keeping last-good",
			"provider", res.ID, "channels", exported, "min_channels", minCh)
		return
	}

	progN := 0
	for _, p := range res.Programmes {
		if p.Title != "" && p.Stop.After(p.Start) {
			progN++
		}
	}
	if progN == 0 {
		slog.Warn("refresh has no programmes; keeping last-good", "provider", res.ID)
		return
	}

	var m3u, xmltv bytes.Buffer
	if err := emit.M3U(&m3u, res.Channels, label); err != nil {
		slog.Warn("m3u emit failed; keeping last-good", "provider", res.ID, "err", err)
		return
	}
	if err := emit.XMLTV(&xmltv, res.Channels, res.Programmes, label); err != nil {
		slog.Warn("xmltv emit failed; keeping last-good", "provider", res.ID, "err", err)
		return
	}

	store.Put(snapshot.Snapshot{
		ProviderID:     res.ID,
		M3U:            m3u.Bytes(),
		XML:            xmltv.Bytes(),
		ChannelCount:   exported,
		ProgrammeCount: progN,
	})
	slog.Info("refresh published",
		"provider", res.ID,
		"channels", exported,
		"programmes", progN,
		"m3u_bytes", m3u.Len(),
		"xml_bytes", xmltv.Len(),
	)
}
