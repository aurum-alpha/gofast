// Package refresh schedules per-provider refreshes: each Refresher fetches its
// provider, classifies + gates the result, emits per-provider m3u/xml, persists
// the last-known-good via the cache, and notifies the aggregator.
package refresh

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/channelattr"
	"github.com/j27-aurum/gofast/internal/classifier"
	"github.com/j27-aurum/gofast/internal/logocache"
	"github.com/j27-aurum/gofast/internal/m3u"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/xmltv"
)

const minInterval = time.Minute

// scheduleHeartbeat is the per-provider next-refresh log interval.
// Tests may shorten it.
var scheduleHeartbeat = 5 * time.Minute

// Refresher is the schedulable unit: one provider's full refresh cycle.
type Refresher interface {
	ID() model.ProviderID
	Interval() time.Duration
	FetchedAt() time.Time
	Refresh(ctx context.Context) error
	ExpectedGuideHorizon() time.Duration
	EmpiricalGuideHorizon() time.Duration
	SetRefreshSchedule(configured, effective time.Duration, clamped bool)
	GuideHoursAhead() float64
	GuideEnd() time.Time
}

// Service schedules a set of Refreshers, each on its own goroutine.
type Service struct {
	refreshers []Refresher
}

// New builds one Refresher per enabled feed. notify is called after each
// successful publish (wire it to the aggregator); refresh never imports aggregate.
// logos may be nil when cache_logos is disabled — logo rewrite runs in the
// background after each publish (see rewriteLogosAndRepublish).
// attrs may be nil; when set, current channel health is Annotate'd before Feed.Set.
// st may be nil; when set, background logo work updates GET /api/status.
func New(reg *provider.Registry, clf *classifier.Client, cc *cache.Cache, policy EmissionPolicy, logos *logocache.Cache, attrs *channelattr.Store, notify func(), st *Status) *Service {
	var rs []Refresher
	for _, f := range reg.Feeds() {
		rs = append(rs, &providerRefresher{feed: f, clf: clf, cache: cc, policy: policy, logos: logos, attrs: attrs, notify: notify, status: st})
	}
	return &Service{refreshers: rs}
}

// Restore rebuilds each feed from its cached raw upstream snapshot — re-parsed
// through the same pipeline as a network fetch (no network) — and overlays the
// persisted classifications from meta.json. So the API and serving work at boot
// exactly as after a fetch. Providers with no cached raw are left empty (they
// fetch on boot); unreadable/unparseable entries are skipped.
//
// attrs may be nil; when set, current health is painted onto channels before Set.
// Logo HTTP is not run here — call WarmLogos in the background after listen so
// /healthz and the UI come up immediately.
func Restore(reg *provider.Registry, cc *cache.Cache, policy EmissionPolicy, attrs *channelattr.Store) {
	for _, f := range reg.Feeds() {
		raw, meta, _, err := cc.LoadProvider(f.ID())
		if err != nil {
			continue // no cached snapshot; will fetch on boot
		}
		if status, ok := cc.LoadStatus(f.ID()); ok {
			if !status.LastErrorAt.IsZero() && !status.LastErrorAt.After(meta.FetchedAt) {
				status.LastError = ""
				status.LastErrorAt = time.Time{}
			}
			f.SetStatus(status)
		}
		pr := &providerRefresher{feed: f, cache: cc, policy: policy, attrs: attrs}
		if err := pr.rehydrate(raw, meta); err != nil {
			slog.Warn("cache restore failed", "provider", f.ID(), "err", err)
			continue
		}
		slog.Info("restored from cache", "provider", f.ID(), "channels", len(f.Channels()), "fetched_at", f.FetchedAt())
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
// loops every interval with +/-10% jitter. A local 5-minute ticker logs the
// next upstream refresh ETA without blocking other providers.
func run(ctx context.Context, r Refresher) {
	interval := applyRefreshClamp(r)
	first := time.Duration(0)
	if fa := r.FetchedAt(); !fa.IsZero() {
		if remaining := interval - time.Since(fa); remaining > 0 {
			first = remaining
		}
	}
	nextRefreshAt := time.Now().Add(first)
	t := time.NewTimer(first)
	defer t.Stop()

	heartbeat := time.NewTicker(scheduleHeartbeat)
	defer heartbeat.Stop()

	refreshing := false
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-heartbeat.C:
			logRefreshSchedule(r.ID(), now, nextRefreshAt, refreshing)
			warnIfGuideExhausted(r)
		case <-t.C:
			refreshing = true
			start := time.Now()
			err := r.Refresh(ctx)
			refreshing = false
			if err != nil {
				slog.Warn("refresh failed; keeping last-good",
					"provider", r.ID(),
					"err", err,
					"duration", time.Since(start),
				)
			}
			interval = applyRefreshClamp(r)
			warnIfGuideExhausted(r)
			delay := jitter(interval)
			nextRefreshAt = time.Now().Add(delay)
			t.Reset(delay)
		}
	}
}

func applyRefreshClamp(r Refresher) time.Duration {
	configured := r.Interval()
	horizon := resolveHorizon(r.EmpiricalGuideHorizon(), r.ExpectedGuideHorizon())
	effective, clamped := ClampInterval(configured, horizon)
	r.SetRefreshSchedule(configured, effective, clamped)
	if clamped {
		slog.Warn("refresh interval clamped to guide horizon",
			"provider", r.ID(),
			"configured", configured,
			"effective", effective,
			"guide_horizon", horizon,
		)
	}
	return effective
}

func warnIfGuideExhausted(r Refresher) {
	if r.GuideEnd().IsZero() {
		return
	}
	ahead := r.GuideHoursAhead()
	configured := r.Interval()
	horizon := resolveHorizon(r.EmpiricalGuideHorizon(), r.ExpectedGuideHorizon())
	effective, _ := ClampInterval(configured, horizon)
	if r.GuideEnd().Before(time.Now()) || ahead*float64(time.Hour) < float64(effective) {
		slog.Warn("guide_horizon_exhausted",
			"provider", r.ID(),
			"guide_hours_ahead", ahead,
			"effective_refresh_interval", effective,
		)
	}
}

func logRefreshSchedule(id model.ProviderID, now, nextRefreshAt time.Time, refreshing bool) {
	if refreshing {
		slog.Info("refresh schedule",
			"provider", id,
			"now", now.UTC(),
			"refresh_in", time.Duration(0),
			"refresh_state", "in_progress",
		)
		return
	}
	remaining := nextRefreshAt.Sub(now)
	if remaining < 0 {
		remaining = 0
	}
	slog.Info("refresh schedule",
		"provider", id,
		"now", now.UTC(),
		"next_refresh_at", nextRefreshAt.UTC(),
		"refresh_in", remaining.Round(time.Second),
	)
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
	policy EmissionPolicy
	logos  *logocache.Cache
	attrs  *channelattr.Store
	notify func()
	status *Status
}

func (p *providerRefresher) ID() model.ProviderID    { return p.feed.ID() }
func (p *providerRefresher) Interval() time.Duration { return p.feed.Interval() }
func (p *providerRefresher) FetchedAt() time.Time    { return p.feed.FetchedAt() }

func (p *providerRefresher) ExpectedGuideHorizon() time.Duration {
	return p.feed.ExpectedGuideHorizon()
}

func (p *providerRefresher) EmpiricalGuideHorizon() time.Duration {
	return p.feed.EmpiricalGuideHorizon()
}

func (p *providerRefresher) SetRefreshSchedule(configured, effective time.Duration, clamped bool) {
	p.feed.SetRefreshSchedule(configured, effective, clamped)
}

func (p *providerRefresher) GuideHoursAhead() float64 {
	return p.feed.Stats().GuideHoursAhead
}

func (p *providerRefresher) GuideEnd() time.Time {
	return p.feed.Stats().GuideEnd
}

// Refresh runs one full network cycle and publishes only after every stage
// succeeds. Any failure records status and leaves the last-known-good untouched.
func (p *providerRefresher) Refresh(ctx context.Context) error {
	start := time.Now()
	status := p.feed.Status()
	status.LastAttemptAt = time.Now()
	p.setStatus(status)

	raw, err := p.feed.Reader().Fetch(ctx)
	if err != nil {
		return p.fail(err, time.Since(start))
	}
	chs, progs, err := p.feed.Reader().Parse(raw)
	if err != nil {
		return p.fail(err, time.Since(start))
	}
	previous := p.feed.Lineup().SyntheticChannelNumbers
	chs, progs, assignments, err := p.transform(chs, progs, previous)
	if err != nil {
		return p.fail(err, time.Since(start))
	}
	if p.clf != nil {
		slog.Info("classifying channels", "provider", p.feed.ID(), "count", len(chs))
		chs = p.clf.ClassifyChannels(ctx, chs)
	}
	lineup, m3uData, xmlData, err := p.prepare(ctx, chs, progs, assignments, time.Now())
	if err != nil {
		return p.fail(err, time.Since(start))
	}
	if err := p.cache.CommitProvider(p.feed.ID(), raw, m3uData, xmlData, provider.MetaOf(lineup)); err != nil {
		return p.fail(err, time.Since(start))
	}
	p.setLineup(lineup)
	status = p.feed.Status()
	status.LastError = ""
	status.LastErrorAt = time.Time{}
	p.setStatus(status)
	duration := time.Since(start)
	p.feed.RecordRefresh(true, duration)
	if p.notify != nil {
		p.notify()
	}
	p.logPublished(lineup, len(m3uData), len(xmlData), duration)
	if p.logos != nil {
		go p.scheduleLogoRewrite(context.WithoutCancel(ctx))
	}
	return nil
}

// rehydrate rebuilds the feed from a cached raw snapshot (no network): parse ->
// transform -> apply persisted classifications -> commit (fetch time from meta).
func (p *providerRefresher) rehydrate(raw provider.Raw, meta provider.Meta) error {
	chs, progs, err := p.feed.Reader().Parse(raw)
	if err != nil {
		return err
	}
	chs, progs, assignments, err := p.transform(chs, progs, meta.SyntheticChannelNumbers)
	if err != nil {
		return err
	}
	for i := range chs {
		if c, ok := meta.Classifications[chs[i].NormalizedID]; ok {
			chs[i].Classification = c
		}
	}
	lineup, m3uData, xmlData, err := p.prepare(context.Background(), chs, progs, assignments, meta.FetchedAt)
	if err != nil {
		return err
	}
	if err := p.cache.CommitProvider(p.feed.ID(), raw, m3uData, xmlData, provider.MetaOf(lineup)); err != nil {
		return err
	}
	p.setLineup(lineup)
	return nil
}

// setLineup paints current channel attrs onto the lineup then publishes to the feed.
func (p *providerRefresher) setLineup(lineup provider.Lineup) {
	if p.attrs != nil {
		lineup.Channels = p.attrs.Annotate(p.feed.ID(), lineup.Channels)
	}
	p.feed.Set(lineup)
}

// transform is the shared decode-time pipeline (identical for the network and
// cache paths): stamp provider, normalize, apply the channel-number offset, mark
// exclusions, and normalize programme channel references.
func (p *providerRefresher) transform(chs []model.Channel, progs []model.Programme, previous provider.ChannelNumberAssignments) ([]model.Channel, []model.Programme, provider.ChannelNumberAssignments, error) {
	id := p.feed.ID()
	s := p.feed.Settings()
	for i := range chs {
		chs[i].Provider = id
		chs[i].Normalize()
		chs[i].ApplyChannelNumberOffset(s.ChannelNumberOffset)
	}
	if err := model.ValidateNormalizedIDs(chs); err != nil {
		return nil, nil, nil, fmt.Errorf("provider %s: %w", id, err)
	}
	chs = model.MarkExclusions(chs, s.ExclusionRegexes)
	assignments, err := previous.Apply(chs, s.SynthesizeChannelNumbers)
	if err != nil {
		return nil, nil, nil, err
	}
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
	return chs, progs, assignments, nil
}

// prepare applies export rules, runs quality gates, and renders one immutable
// candidate without mutating the feed or cache.
func (p *providerRefresher) prepare(ctx context.Context, chs []model.Channel, progs []model.Programme, assignments provider.ChannelNumberAssignments, fetchedAt time.Time) (provider.Lineup, cache.M3U, cache.XMLTV, error) {
	id := p.feed.ID()
	s := p.feed.Settings()
	var emission emissionStats
	chs, emission = applyEmissionPolicy(chs, p.policy)
	if emission.NeedsProxyDropped > 0 {
		slog.Warn("channels need FASTProxy; dropping from export",
			"provider", id,
			"count", emission.NeedsProxyDropped,
		)
	}

	label := s.Label
	if label == "" {
		label = string(id)
	}

	exported := len(model.ForExport(chs))
	minCh := s.MinChannels
	if minCh <= 0 {
		minCh = 1
	}
	if exported < minCh {
		return provider.Lineup{}, nil, nil, fmt.Errorf("provider %s: %d exported channels below min_channels %d", id, exported, minCh)
	}

	exportedIDs := make(map[string]struct{}, exported)
	for _, ch := range model.ForExport(chs) {
		exportedIDs[ch.NormalizedID] = struct{}{}
	}
	progN := 0
	for _, pr := range progs {
		if _, ok := exportedIDs[pr.ChannelID]; ok && pr.IsValid() {
			progN++
		}
	}
	if progN == 0 {
		return provider.Lineup{}, nil, nil, fmt.Errorf("provider %s: no exportable programmes", id)
	}

	m3uData, err := m3u.Marshal(chs, label)
	if err != nil {
		return provider.Lineup{}, nil, nil, err
	}
	xmlData, err := xmltv.Marshal(chs, progs, label)
	if err != nil {
		return provider.Lineup{}, nil, nil, err
	}

	lineup := provider.Lineup{
		Channels:                chs,
		Programmes:              progs,
		ChannelCount:            exported,
		ProgrammeCount:          progN,
		FetchedAt:               fetchedAt,
		SyntheticChannelNumbers: assignments,
	}
	return lineup, cache.M3U(m3uData), cache.XMLTV(xmlData), nil
}

func (p *providerRefresher) fail(err error, duration time.Duration) error {
	status := p.feed.Status()
	status.LastError = err.Error()
	status.LastErrorAt = time.Now()
	p.setStatus(status)
	p.feed.RecordRefresh(false, duration)
	return err
}

func (p *providerRefresher) logPublished(lineup provider.Lineup, m3uBytes, xmlBytes int, duration time.Duration) {
	horizon := resolveHorizon(p.feed.EmpiricalGuideHorizon(), p.feed.ExpectedGuideHorizon())
	configured := p.feed.Interval()
	effective, _ := ClampInterval(configured, horizon)
	slog.Info("published",
		"provider", p.feed.ID(),
		"channels", lineup.ChannelCount,
		"programmes", lineup.ProgrammeCount,
		"m3u_bytes", m3uBytes,
		"xml_bytes", xmlBytes,
		"duration", duration,
		"guide_horizon", horizon,
		"refresh_interval", configured,
		"effective_interval", effective,
	)
}

func (p *providerRefresher) setStatus(status provider.Status) {
	p.feed.SetStatus(status)
	if err := p.cache.WriteStatus(p.feed.ID(), status); err != nil {
		slog.Warn("status write failed", "provider", p.feed.ID(), "err", err)
	}
}
