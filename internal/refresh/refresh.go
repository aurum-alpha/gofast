// Package refresh runs provider FetchAll, classifies streams, writes playlists, and updates snapshots.
package refresh

import (
	"bytes"
	"context"
	"log/slog"
	"time"

	"github.com/j27-aurum/gofast/internal/classifier"
	"github.com/j27-aurum/gofast/internal/m3u"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/snapshot"
	"github.com/j27-aurum/gofast/internal/xmltv"
)

// Once fetches all enabled providers, classifies streams, and publishes gated snapshots.
func Once(ctx context.Context, reg *provider.Registry, store *snapshot.Store, clf *classifier.Client) {
	if reg == nil || store == nil {
		return
	}
	for _, res := range reg.FetchAll(ctx) {
		publish(ctx, reg, store, clf, res)
	}
}

// Loop periodically calls Once until ctx is cancelled.
// Caller should invoke Once once before starting Loop if a warm snapshot is required at boot.
// Thin vertical-slice behavior: in-memory only, min_channels + programme gates,
// keep last-good on failure. Full jitter/disk LKG lands in J27-18.
func Loop(ctx context.Context, reg *provider.Registry, store *snapshot.Store, clf *classifier.Client) {
	if reg == nil || store == nil {
		return
	}
	interval := refreshInterval(reg)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			Once(ctx, reg, store, clf)
		}
	}
}

func refreshInterval(reg *provider.Registry) time.Duration {
	d := 6 * time.Hour
	for _, id := range reg.IDs() {
		if ri := reg.Settings(id).RefreshInterval; ri > 0 && ri < d {
			d = ri
		}
	}
	if d < time.Minute {
		return time.Minute
	}
	return d
}

func publish(ctx context.Context, reg *provider.Registry, store *snapshot.Store, clf *classifier.Client, res provider.Result) {
	pcfg := reg.Settings(res.ID)
	label := pcfg.Label
	if label == "" {
		label = res.ID
	}

	if res.Err != nil {
		slog.Warn("refresh failed; keeping last-good", "provider", res.ID, "err", res.Err)
		return
	}

	channels := res.Channels
	if clf != nil {
		slog.Info("classifying channels", "provider", res.ID, "count", len(channels))
		channels = clf.ClassifyChannels(ctx, channels)
		channels = applyClassificationExport(channels)
	}

	exported := 0
	for _, ch := range channels {
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

	var m3uBuf, xmltvBuf bytes.Buffer
	if err := m3u.Write(&m3uBuf, channels, label); err != nil {
		slog.Warn("m3u write failed; keeping last-good", "provider", res.ID, "err", err)
		return
	}
	if err := xmltv.Write(&xmltvBuf, channels, res.Programmes, label); err != nil {
		slog.Warn("xmltv write failed; keeping last-good", "provider", res.ID, "err", err)
		return
	}

	store.Put(snapshot.Snapshot{
		ProviderID:     res.ID,
		Label:          label,
		M3U:            m3uBuf.Bytes(),
		XML:            xmltvBuf.Bytes(),
		Channels:       channels,
		Programmes:     res.Programmes,
		ChannelCount:   exported,
		ProgrammeCount: progN,
	})
	slog.Info("refresh published",
		"provider", res.ID,
		"channels", exported,
		"programmes", progN,
		"m3u_bytes", m3uBuf.Len(),
		"xml_bytes", xmltvBuf.Len(),
	)
}

// applyClassificationExport drops DRM always. BEACON hard-drop when proxy is
// unset lands with proxy_base_url emission (J27-19); until then badges show
// BEACON while playlists stay exportable for the LG vertical slice.
func applyClassificationExport(channels []model.Channel) []model.Channel {
	out := make([]model.Channel, len(channels))
	copy(out, channels)
	for i := range out {
		if out[i].Classification == model.ClassDRM {
			out[i].Excluded = true
			if out[i].FilterReason == "" {
				out[i].FilterReason = "DRM"
			}
		}
	}
	return out
}
