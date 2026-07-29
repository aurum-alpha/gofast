package refresh

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"reflect"
	"sync"
	"time"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/categories"
	"github.com/j27-aurum/gofast/internal/channelattr"
	"github.com/j27-aurum/gofast/internal/classifier"
	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/groups"
	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/logocache"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/providerset"
)

// pipeline is the shared, hot-reloadable emit environment for every
// providerRefresher: the emission policy, compiled taxonomies, and the logo
// cache (nil when cache_logos is off). Refreshers snapshot it once per
// operation.
type pipeline struct {
	mu         sync.RWMutex
	policy     EmissionPolicy
	groups     *groups.Policy
	categories *categories.Policy
	logos      *logocache.Cache
}

// snapshot returns the current emit environment (nil pipeline is all-zero).
func (e *pipeline) snapshot() (EmissionPolicy, *groups.Policy, *categories.Policy, *logocache.Cache) {
	if e == nil {
		return EmissionPolicy{}, nil, nil, nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.policy, e.groups, e.categories, e.logos
}

// set atomically replaces the emit environment.
func (e *pipeline) set(policy EmissionPolicy, gp *groups.Policy, cp *categories.Policy, logos *logocache.Cache) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.policy = policy
	e.groups = gp
	e.categories = cp
	e.logos = logos
}

// runningProvider is one supervised provider goroutine.
type runningProvider struct {
	pr     *providerRefresher
	cancel context.CancelFunc
	kick   chan struct{} // wakes the schedule loop to recompute its timer
}

// Service supervises one refresh goroutine per enabled provider and implements
// config.Reloader: on a config save it reconciles providers (start/stop/reload),
// the emission policy, the group taxonomy, and the logo cache against the new
// snapshot. notify is called after publishes (wire it to the aggregator);
// refresh never imports aggregate.
type Service struct {
	store   *config.Store // nil in tests: static zero policy, no groups/logos
	reg     *provider.Registry
	clf     *classifier.Client
	cache   *cache.Cache
	attrs   *channelattr.Store
	attrBus channelattr.Bus
	notify  func()
	status  *Status
	pipe    *pipeline

	mu       sync.Mutex
	client   *httpx.Client
	logosSrc *logocache.Cache // constructed regardless of the cache_logos gate
	runCtx   context.Context
	running  map[model.ProviderID]*runningProvider
	applied  *config.Config // last-applied snapshot for Reload diffs
	tally    RefreshTally
}

// RefreshTally records durable refresh success/fail counts (ops report).
type RefreshTally interface {
	Inc(provider model.ProviderID, ok bool)
}

// SetRefreshTally wires an optional tally sink (e.g. opsreport.Scheduler).
func (s *Service) SetRefreshTally(t RefreshTally) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tally = t
}

// New builds the provider supervisor. The config snapshot from store seeds the
// emission policy, group taxonomy, and logo cache; Reload keeps them current.
// clf/attrs/attrBus/st may be nil (feature off). A nil store (tests) yields a
// zero emission policy with groups and logos off.
func New(store *config.Store, reg *provider.Registry, clf *classifier.Client, cc *cache.Cache, client *httpx.Client, attrs *channelattr.Store, attrBus channelattr.Bus, notify func(), st *Status) *Service {
	s := &Service{
		store:   store,
		reg:     reg,
		clf:     clf,
		cache:   cc,
		attrs:   attrs,
		attrBus: attrBus,
		notify:  notify,
		status:  st,
		pipe:    &pipeline{},
		client:  client,
		running: map[model.ProviderID]*runningProvider{},
	}
	if store != nil {
		cfg := store.Current()
		s.logosSrc = buildLogoCache(cfg, cc)
		s.pipe.set(emissionPolicyFrom(cfg), groups.Compile(cfg.Groups), categories.Compile(cfg.Categories), s.gateLogos(cfg))
		s.applied = cfg
	}
	return s
}

// GroupsPolicy returns the live compiled group taxonomy (for API handlers).
func (s *Service) GroupsPolicy() *groups.Policy {
	_, gp, _, _ := s.pipe.snapshot()
	return gp
}

// CategoriesPolicy returns the live compiled category taxonomy (for API handlers).
func (s *Service) CategoriesPolicy() *categories.Policy {
	_, _, cp, _ := s.pipe.snapshot()
	return cp
}

// ReapplyAll re-emits every running provider from its cached raw snapshot (no
// network), recomputing taxonomy + exclusions with the current pipeline, then
// republishing. Per-provider failures are logged and skipped.
func (s *Service) ReapplyAll() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reapplyRunningLocked()
}

// Reload implements config.Reloader: reconcile providers and the emit
// environment against the new snapshot, reapplying cached lineups only for the
// slices that actually changed.
func (s *Service) Reload(ctx context.Context, cfg *config.Config) error {
	if s == nil || cfg == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	prev := s.applied
	if prev == nil {
		prev = &config.Config{}
	}

	// Outbound HTTP timeout: rebuild the shared client; readers pick it up via
	// the per-provider reload below (settings compare is forced).
	clientChanged := prev.Timeouts.HTTPClient != cfg.Timeouts.HTTPClient
	if clientChanged {
		s.client = httpx.NewClient(cfg.Timeouts.HTTPClient, 0)
		slog.Info("outbound http client rebuilt", "timeout", cfg.Timeouts.HTTPClient.String())
	}

	// Logo cache identity: base_url / artwork_tls changes rebuild it.
	logosRebuilt := prev.BaseURL != cfg.BaseURL || !reflect.DeepEqual(prev.ArtworkTLS, cfg.ArtworkTLS)
	if logosRebuilt {
		s.logosSrc = buildLogoCache(cfg, s.cache)
	}

	reapplyAll := false
	warm := false
	changed := false

	// Provider reconcile: start newly enabled, stop newly disabled, reload
	// settings changes on running providers.
	desired := providerset.Settings(cfg.Providers)
	var perProvider []model.ProviderID
	for _, id := range providerset.Known() {
		des := desired[id]
		rp := s.running[id]
		switch {
		case des.IsEnabled() && rp == nil:
			s.startProviderLocked(id, des)
			changed = true
		case !des.IsEnabled() && rp != nil:
			s.stopProviderLocked(id, des)
			changed = true
		case des.IsEnabled():
			cur := rp.pr.feed.Settings()
			if !clientChanged && cur.Equal(des) {
				continue
			}
			reader, ok := providerset.Reader(id, des, s.client)
			if !ok {
				continue
			}
			s.reg.Upsert(id, reader, des)
			perProvider = append(perProvider, id)
			if cur.RefreshInterval != des.RefreshInterval {
				select {
				case rp.kick <- struct{}{}:
				default:
				}
			}
			slog.Info("provider settings reloaded", "id", id)
			changed = true
		default:
			s.reg.UpdateSettings(id, des)
		}
	}

	// Emission / groups / categories / cache_logos slices.
	policy := emissionPolicyFrom(cfg)
	if emissionPolicyFrom(prev) != policy {
		reapplyAll = true
	}
	if !reflect.DeepEqual(prev.Groups, cfg.Groups) {
		reapplyAll = true
	}
	if !reflect.DeepEqual(prev.Categories, cfg.Categories) {
		reapplyAll = true
	}
	wasLogos := prev.CacheLogosEnabled()
	nowLogos := cfg.CacheLogosEnabled()
	if wasLogos != nowLogos {
		if nowLogos {
			warm = true // one background warm pass rewrites + republishes
		} else {
			reapplyAll = true // re-parse raw so LogoURL reverts upstream
		}
	}
	// base_url / artwork_tls change rebuilds the logo cache identity; reapply
	// alone would republish CDN URLs under the old rewrite base — warm after.
	if logosRebuilt && nowLogos {
		reapplyAll = true
		warm = true
	}
	// Provider settings reapply (incl. channel_emit logo_url) leaves LogoURL at
	// the operator/CDN value until a warm pass caches + rewrites it.
	if nowLogos && len(perProvider) > 0 {
		warm = true
	}
	s.pipe.set(policy, groups.Compile(cfg.Groups), categories.Compile(cfg.Categories), s.gateLogos(cfg))
	s.applied = cfg

	if reapplyAll {
		s.reapplyRunningLocked()
		changed = true
	} else {
		for _, id := range perProvider {
			rp := s.running[id]
			if rp == nil {
				continue
			}
			if err := rp.pr.reapplyFromCache(); err != nil && !errors.Is(err, fs.ErrNotExist) {
				slog.Warn("provider reapply failed; keeping last-good", "provider", id, "err", err)
			}
		}
	}
	if changed && s.notify != nil {
		s.notify()
	}
	if warm {
		ctx := s.runCtx
		if ctx == nil {
			ctx = context.Background()
		}
		go s.WarmLogos(ctx)
	}
	return nil
}

// Restore rebuilds each feed from its cached raw upstream snapshot — re-parsed
// through the same pipeline as a network fetch (no network). Classifications
// come from the channel-attr store (Annotate); URL dialect heuristics
// (SESSION / XUMO_SSAI) then override when the stream URL shape is definitive.
// Legacy meta.json maps are seeded into the store once when Current is missing.
// Providers with no cached raw are left empty (they fetch on boot).
//
// Call Restore before starting channelattr.Receive so meta seed Handle calls
// do not race the AttrReceiver writer. Logo HTTP is not run here — call
// WarmLogos in the background after listen.
func (s *Service) Restore() {
	for _, f := range s.reg.Feeds() {
		raw, meta, _, err := s.cache.LoadProvider(f.ID())
		if err != nil {
			continue // no cached snapshot; will fetch on boot
		}
		if status, ok := s.cache.LoadStatus(f.ID()); ok {
			if !status.LastErrorAt.IsZero() && !status.LastErrorAt.After(meta.FetchedAt) {
				status.LastError = ""
				status.LastErrorAt = time.Time{}
			}
			f.SetStatus(status)
		}
		pr := s.newRefresher(f)
		pr.attrBus = nil // seed attrs directly; Receive is not running yet
		if err := pr.rehydrate(raw, meta); err != nil {
			slog.Warn("cache restore failed", "provider", f.ID(), "err", err)
			continue
		}
		slog.Info("restored from cache", "provider", f.ID(), "channels", len(f.Channels()), "fetched_at", f.FetchedAt())
	}
}

// Run launches one schedule goroutine per enabled provider and retains ctx as
// the parent for providers started later by Reload. It returns immediately.
func (s *Service) Run(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runCtx = ctx
	for _, f := range s.reg.Feeds() {
		if _, ok := s.running[f.ID()]; ok {
			continue
		}
		s.launchLocked(s.newRefresher(f))
	}
}

// TriggerAsync starts one network refresh for id in a background goroutine.
// Returns ErrUnknownProvider or ErrRefreshInFlight without starting work.
// runCtx should be the process lifetime context (cancelled on shutdown).
func (s *Service) TriggerAsync(runCtx context.Context, id model.ProviderID) error {
	if s == nil {
		return ErrUnknownProvider
	}
	s.mu.Lock()
	rp, ok := s.running[id]
	s.mu.Unlock()
	if !ok {
		return ErrUnknownProvider
	}
	p := rp.pr
	if err := p.beginRefresh(); err != nil {
		return err
	}
	go func() {
		defer p.endRefresh()
		start := time.Now()
		err := p.refreshLocked(runCtx)
		if err != nil {
			slog.Warn("on-demand refresh failed; keeping last-good",
				"provider", id,
				"err", err,
				"duration", time.Since(start),
			)
			return
		}
		slog.Info("on-demand refresh completed",
			"provider", id,
			"duration", time.Since(start),
		)
	}()
	return nil
}

// gateLogos returns the logo cache when cache_logos is enabled, else nil.
func (s *Service) gateLogos(cfg *config.Config) *logocache.Cache {
	if !cfg.CacheLogosEnabled() {
		return nil
	}
	return s.logosSrc
}

// launchLocked registers and starts the schedule goroutine for pr. Caller holds s.mu.
func (s *Service) launchLocked(pr *providerRefresher) {
	base := s.runCtx
	if base == nil {
		base = context.Background()
	}
	pctx, cancel := context.WithCancel(base)
	rp := &runningProvider{pr: pr, cancel: cancel, kick: make(chan struct{}, 1)}
	s.running[pr.ID()] = rp
	go run(pctx, pr, rp.kick)
}

// newRefresher composes a feed with the shared pipeline and sinks.
func (s *Service) newRefresher(f *provider.Feed) *providerRefresher {
	return &providerRefresher{
		feed:    f,
		clf:     s.clf,
		cache:   s.cache,
		pipe:    s.pipe,
		attrs:   s.attrs,
		attrBus: s.attrBus,
		notify:  s.notify,
		status:  s.status,
		tally:   s.tally,
	}
}

// reapplyRunningLocked re-emits every running provider from cache. Caller holds s.mu.
func (s *Service) reapplyRunningLocked() {
	for id, rp := range s.running {
		if err := rp.pr.reapplyFromCache(); err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("reapply failed; keeping last-good", "provider", id, "err", err)
		}
	}
}

// startProviderLocked enables a provider live: build its reader, upsert the
// feed, restore instantly from cache when a raw snapshot exists (warm) or leave
// it empty for the immediate scheduled fetch (cold), then launch its goroutine.
// Caller holds s.mu.
func (s *Service) startProviderLocked(id model.ProviderID, des model.ProviderSettings) {
	reader, ok := providerset.Reader(id, des, s.client)
	if !ok {
		slog.Warn("provider has no implementation; cannot enable", "id", id)
		return
	}
	feed := s.reg.Upsert(id, reader, des)
	pr := s.newRefresher(feed)
	if status, ok := s.cache.LoadStatus(id); ok {
		feed.SetStatus(status)
	}
	switch err := pr.reapplyFromCache(); {
	case err == nil:
		slog.Info("provider enabled; restored from cache",
			"id", id, "channels", len(feed.Channels()), "fetched_at", feed.FetchedAt())
	case errors.Is(err, fs.ErrNotExist):
		slog.Info("provider enabled; no cached snapshot, fetching", "id", id)
	default:
		slog.Warn("provider enabled; cache restore failed, fetching", "id", id, "err", err)
	}
	s.launchLocked(pr)
}

// stopProviderLocked disables a provider live: cancel its goroutine (stopping
// scheduled and in-flight fetches) and drop its feed from the registry. The
// cache generations (synthetic channel numbers, last-good) and channel-attr
// history stay on disk, so re-enabling is lossless. Caller holds s.mu.
func (s *Service) stopProviderLocked(id model.ProviderID, des model.ProviderSettings) {
	if rp, ok := s.running[id]; ok {
		rp.cancel()
		delete(s.running, id)
	}
	s.reg.Remove(id, des)
	slog.Info("provider disabled; cache and channel attributes kept", "id", id)
}

// buildLogoCache constructs the logo cache for the current snapshot (artwork
// TLS client + public base URL). It is built regardless of the cache_logos
// gate so a live enable needs no reconstruction. Returns nil (with a warning)
// when the artwork client cannot be built.
func buildLogoCache(cfg *config.Config, cc *cache.Cache) *logocache.Cache {
	hosts := make(map[string]logocache.HostPolicy, len(cfg.ArtworkTLS))
	for host, policy := range cfg.ArtworkTLS {
		hosts[host] = logocache.HostPolicy{
			CAPem:              policy.CAPem,
			InsecureSkipVerify: policy.InsecureSkipVerify,
		}
	}
	client, err := logocache.NewArtworkClient(cfg.Timeouts.HTTPClient, hosts)
	if err != nil {
		slog.Warn("artwork http client build failed; logo caching unavailable", "err", err)
		return nil
	}
	return logocache.New(cc, client, cfg.BaseURL, 0)
}

// emissionPolicyFrom maps the config snapshot to the emit-time policy.
func emissionPolicyFrom(cfg *config.Config) EmissionPolicy {
	if cfg == nil {
		return EmissionPolicy{}
	}
	return EmissionPolicy{
		ProxyBaseURL:     cfg.ProxyBaseURL,
		ProxyAll:         cfg.ProxyAllEnabled(),
		ExcludeUnhealthy: cfg.HealthExcludeUnhealthy(),
	}
}
