package refresh

import (
	"context"
	"log/slog"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/logocache"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

// WarmLogos rewrites logos for every restored feed in the background. Safe to
// call with a nil logos cache (no-op). Updates st for GET /api/status.
func WarmLogos(ctx context.Context, reg *provider.Registry, cc *cache.Cache, policy EmissionPolicy, logos *logocache.Cache, notify func(), st *Status) {
	if logos == nil {
		return
	}
	feeds := reg.Feeds()
	total := 0
	for _, f := range feeds {
		total += countLogoTargets(f.Channels())
	}
	if total == 0 {
		return
	}
	st.SetLogos(true, 0, total, "")
	defer st.SetLogos(false, total, total, "")

	done := 0
	for _, f := range feeds {
		if ctx.Err() != nil {
			return
		}
		pr := &providerRefresher{feed: f, cache: cc, policy: policy, logos: logos, notify: notify}
		_, err := pr.rewriteLogosAndRepublish(ctx, func() {
			done++
			st.SetLogos(true, done, total, string(f.ID()))
		})
		if err != nil {
			slog.Warn("background logo rewrite failed", "provider", f.ID(), "err", err)
			continue
		}
		slog.Info("background logos published", "provider", f.ID())
	}
}

func countLogoTargets(chs []model.Channel) int {
	n := 0
	for _, ch := range chs {
		if ch.LogoURL != "" || ch.LogoSourceURL != "" {
			n++
		}
	}
	return n
}

// scheduleLogoRewrite runs logo rewrite+republish after a successful refresh.
func (p *providerRefresher) scheduleLogoRewrite(ctx context.Context) {
	total := countLogoTargets(p.feed.Channels())
	if total == 0 {
		return
	}
	done := 0
	if p.status != nil {
		p.status.SetLogos(true, 0, total, string(p.feed.ID()))
		defer func() {
			p.status.SetLogos(false, done, total, "")
		}()
	}
	n, err := p.rewriteLogosAndRepublish(ctx, func() {
		done++
		if p.status != nil {
			p.status.SetLogos(true, done, total, string(p.feed.ID()))
		}
	})
	if err != nil {
		slog.Warn("background logo rewrite failed", "provider", p.feed.ID(), "err", err, "done", n)
		return
	}
	slog.Info("background logos published", "provider", p.feed.ID(), "logos", n)
}

// rewriteLogosAndRepublish downloads/revalidates logos then re-emits M3U/XML and
// updates the live feed. onEach is called once per logo target after Ensure.
// Returns the number of logo targets processed.
func (p *providerRefresher) rewriteLogosAndRepublish(ctx context.Context, onEach func()) (int, error) {
	if p.logos == nil {
		return 0, nil
	}
	lineup := p.feed.Lineup()
	if len(lineup.Channels) == 0 {
		return 0, nil
	}
	chs := append([]model.Channel(nil), lineup.Channels...)
	for i := range chs {
		if chs[i].LogoSourceURL != "" {
			chs[i].LogoURL = chs[i].LogoSourceURL
		}
	}
	targets := countLogoTargets(chs)
	if targets == 0 {
		return 0, nil
	}
	p.logos.RewriteProgress(ctx, chs, onEach)

	prepared, m3uData, xmlData, err := p.prepare(ctx, chs, lineup.Programmes, lineup.SyntheticChannelNumbers, lineup.FetchedAt)
	if err != nil {
		return targets, err
	}
	raw, err := p.cache.ReadRaw(p.feed.ID())
	if err != nil {
		return targets, err
	}
	if err := p.cache.CommitProvider(p.feed.ID(), raw, m3uData, xmlData, provider.MetaOf(prepared)); err != nil {
		return targets, err
	}
	p.feed.Set(prepared)
	if p.notify != nil {
		p.notify()
	}
	return targets, nil
}
