// Package refresh schedules per-provider refreshes: each Refresher fetches its
// provider, classifies + gates the result, emits per-provider m3u/xml, persists
// the last-known-good via the cache, and notifies the aggregator.
package refresh

import (
	"bytes"
	"context"
	"log/slog"
	"math/rand"
	"time"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/classifier"
	"github.com/j27-aurum/gofast/internal/m3u"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/xmltv"
)

const minInterval = time.Minute

// Refresher is the schedulable unit: one provider's full refresh cycle.
type Refresher interface {
	ID() model.ProviderID
	Interval() time.Duration
	FetchedAt() time.Time
	Refresh(ctx context.Context) error
}

// Service schedules a set of Refreshers, each on its own goroutine.
type Service struct {
	refreshers []Refresher
}

// New builds one Refresher per enabled feed. notify is called after each
// successful publish (wire it to the aggregator); refresh never imports aggregate.
func New(reg *provider.Registry, clf *classifier.Client, cc *cache.Cache, notify func()) *Service {
	var rs []Refresher
	for _, f := range reg.Feeds() {
		rs = append(rs, &providerRefresher{feed: f, clf: clf, cache: cc, notify: notify})
	}
	return &Service{refreshers: rs}
}

// Restore rebuilds each feed from its cached raw upstream snapshot — re-parsed
// through the same pipeline as a network fetch (no network) — and overlays the
// persisted classifications from meta.json. So the API and serving work at boot
// exactly as after a fetch. Providers with no cached raw are left empty (they
// fetch on boot); unreadable/unparseable entries are skipped.
func Restore(reg *provider.Registry, cc *cache.Cache) {
	for _, f := range reg.Feeds() {
		raw, err := cc.ReadRaw(f.ID())
		if err != nil {
			continue // no cached snapshot; will fetch on boot
		}
		meta, _ := cc.LoadMeta(f.ID())
		pr := &providerRefresher{feed: f, cache: cc}
		if err := pr.rehydrate(raw, meta); err != nil {
			slog.Warn("cache restore failed", "id", f.ID(), "err", err)
			continue
		}
		slog.Info("restored from cache", "id", f.ID(), "channels", len(f.Channels()), "fetched_at", f.FetchedAt())
	}
}

// Run starts one goroutine per Refresher until ctx is cancelled.
func (s *Service) Run(ctx context.Context) {
	for _, r := range s.refreshers {
		go run(ctx, r)
	}
}

// run schedules one Refresher: first fire is derived from the persisted
// FetchedAt (so a restart mid-interval resumes rather than restarting), then it
// loops every interval with +/-10% jitter.
func run(ctx context.Context, r Refresher) {
	interval := r.Interval()
	if interval < minInterval {
		interval = 6 * time.Hour
	}
	first := time.Duration(0)
	if fa := r.FetchedAt(); !fa.IsZero() {
		if remaining := interval - time.Since(fa); remaining > 0 {
			first = remaining
		}
	}
	t := time.NewTimer(first)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := r.Refresh(ctx); err != nil {
				slog.Warn("refresh failed; keeping last-good", "id", r.ID(), "err", err)
			}
			t.Reset(jitter(interval))
		}
	}
}

// jitter returns d +/-10%, floored at minInterval.
func jitter(d time.Duration) time.Duration {
	delta := time.Duration((rand.Float64()*2 - 1) * 0.1 * float64(d))
	j := d + delta
	if j < minInterval {
		return minInterval
	}
	return j
}

// providerRefresher composes a feed with the shared pipeline (classify, gate,
// emit, persist, notify). It never lives in the adapter packages.
type providerRefresher struct {
	feed   *provider.Feed
	clf    *classifier.Client
	cache  *cache.Cache
	notify func()
}

func (p *providerRefresher) ID() model.ProviderID    { return p.feed.ID() }
func (p *providerRefresher) Interval() time.Duration { return p.feed.Interval() }
func (p *providerRefresher) FetchedAt() time.Time    { return p.feed.FetchedAt() }

// Refresh runs one full network cycle: fetch (archives raw) -> transform ->
// classify (network) -> commit. Fetch errors are returned; gate failures keep
// the last-good lineup and return nil.
func (p *providerRefresher) Refresh(ctx context.Context) error {
	chs, progs, err := p.feed.Reader().Fetch(ctx)
	if err != nil {
		return err
	}
	chs, progs = p.transform(chs, progs)
	if p.clf != nil {
		slog.Info("classifying channels", "provider", p.feed.ID(), "count", len(chs))
		chs = p.clf.ClassifyChannels(ctx, chs)
	}
	return p.commit(chs, progs, time.Now(), true)
}

// rehydrate rebuilds the feed from a cached raw snapshot (no network): parse ->
// transform -> apply persisted classifications -> commit (fetch time from meta).
func (p *providerRefresher) rehydrate(raw []byte, meta provider.Meta) error {
	chs, progs, err := p.feed.Reader().Parse(raw)
	if err != nil {
		return err
	}
	chs, progs = p.transform(chs, progs)
	for i := range chs {
		if c, ok := meta.Classifications[chs[i].NormalizedID]; ok {
			chs[i].Classification = c
		}
	}
	return p.commit(chs, progs, meta.FetchedAt, false)
}

// transform is the shared decode-time pipeline (identical for the network and
// cache paths): stamp provider, normalize, apply the channel-number offset, mark
// exclusions, and normalize programme channel references.
func (p *providerRefresher) transform(chs []model.Channel, progs []model.Programme) ([]model.Channel, []model.Programme) {
	id := p.feed.ID()
	s := p.feed.Settings()
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
	return chs, progs
}

// commit is the shared tail: apply classification export rules, run gates, emit
// per-provider files, update the feed, and persist meta. notify triggers the
// aggregator (true for a live refresh; false at boot, where main rebuilds once).
func (p *providerRefresher) commit(chs []model.Channel, progs []model.Programme, fetchedAt time.Time, notify bool) error {
	id := p.feed.ID()
	s := p.feed.Settings()
	chs = applyClassificationExport(chs)

	label := s.Label
	if label == "" {
		label = string(id)
	}

	exported := 0
	for _, ch := range chs {
		if !ch.Excluded && ch.NormalizedID != "" && ch.StreamURL != "" {
			exported++
		}
	}
	minCh := s.MinChannels
	if minCh <= 0 {
		minCh = 1
	}
	if exported < minCh {
		slog.Warn("below min_channels; keeping last-good", "provider", id, "channels", exported, "min_channels", minCh)
		return nil
	}

	progN := 0
	for _, pr := range progs {
		if pr.Title != "" && pr.Stop.After(pr.Start) {
			progN++
		}
	}
	if progN == 0 {
		slog.Warn("no programmes; keeping last-good", "provider", id)
		return nil
	}

	var m3uBuf, xmltvBuf bytes.Buffer
	if err := m3u.Write(&m3uBuf, chs, label); err != nil {
		slog.Warn("m3u write failed; keeping last-good", "provider", id, "err", err)
		return nil
	}
	if err := xmltv.Write(&xmltvBuf, chs, progs, label); err != nil {
		slog.Warn("xmltv write failed; keeping last-good", "provider", id, "err", err)
		return nil
	}

	lineup := provider.Lineup{
		Channels:       chs,
		Programmes:     progs,
		ChannelCount:   exported,
		ProgrammeCount: progN,
		FetchedAt:      fetchedAt,
	}
	p.feed.Set(lineup)
	if err := p.cache.WriteProvider(id, cache.M3U(m3uBuf.Bytes()), cache.XMLTV(xmltvBuf.Bytes()), provider.MetaOf(lineup)); err != nil {
		slog.Warn("cache write failed", "provider", id, "err", err)
	}
	if notify && p.notify != nil {
		p.notify()
	}
	slog.Info("published",
		"provider", id,
		"channels", exported,
		"programmes", progN,
		"m3u_bytes", m3uBuf.Len(),
		"xml_bytes", xmltvBuf.Len(),
	)
	return nil
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
