package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/j27-aurum/gofast/internal/aggregate"
	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/channelattr"
	"github.com/j27-aurum/gofast/internal/classifier"
	"github.com/j27-aurum/gofast/internal/clientaccess"
	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/health"
	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/opsreport"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/providerset"
	"github.com/j27-aurum/gofast/internal/proxyactivity"
	"github.com/j27-aurum/gofast/internal/refresh"
	"github.com/j27-aurum/gofast/internal/run"
	"github.com/j27-aurum/gofast/internal/server"
	"github.com/j27-aurum/gofast/internal/ui"
)

func main() {
	run.SetupLogger("fastgen")
	ctx, stop := run.SignalContext(context.Background())
	defer stop()

	store := config.NewStore(configPath())
	if err := store.Load(); err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	cfg := store.Current()
	run.SetLogLevel(cfg.Logging.Level)
	cfg.LogLoaded(store.Path(), store.FromFile())

	client := httpx.NewClient(cfg.Timeouts.HTTPClient, 0)
	cacheDir := filepath.Join(cfg.DataDir, "cache")
	cc := cache.New(cacheDir)

	access, err := clientaccess.Open(cacheDir)
	if err != nil {
		slog.Error("clientaccess open", "err", err)
		os.Exit(1)
	}
	defer access.Close()

	proxyAct, err := proxyactivity.Open(cacheDir)
	if err != nil {
		slog.Error("proxyactivity open", "err", err)
		os.Exit(1)
	}
	defer proxyAct.Close()

	attrs, err := channelattr.Open(cfg.DataDir)
	if err != nil {
		slog.Error("channelattr open", "err", err)
		os.Exit(1)
	}
	defer attrs.Close()
	if err := attrs.LoadCurrent(); err != nil {
		slog.Error("channelattr load current", "err", err)
		os.Exit(1)
	}

	// Providers are code: each known provider's package defaults are overlaid by
	// its YAML settings. No provider runs without its own YAML block — the
	// generated default config enables nothing. Unknown YAML ids have no
	// implementation (warned inside providerset.Settings).
	settings := providerset.Settings(cfg.Providers, cfg.EffectiveRegions())
	readers := providerset.Readers(settings, client)
	reg := provider.NewRegistry(readers, settings)
	reg.LogLoaded()

	bootStatus := &refresh.Status{}
	agg := aggregate.New(reg, cc)
	attrBus := channelattr.NewBus(256)
	clf := classifier.New(client, 0)
	svc := refresh.New(store, reg, clf, cc, client, attrs, attrBus, agg.Notify, bootStatus)

	// Warm feeds from disk (may seed legacy meta classifications into attrs),
	// regenerate the aggregate, then start AttrReceiver + refresh.
	svc.Restore()

	go channelattr.Receive(ctx, attrBus, attrs)

	healthEmitter := &health.Emitter{
		Bus:                 attrBus,
		Store:               attrs,
		ConsecutiveFailures: cfg.HealthConsecutiveFailures(),
	}
	probeClient := httpx.NewClient(cfg.Timeouts.HTTPClient, 1)
	softRetries := cfg.HealthSoftRetries()
	sched := &health.Scheduler{
		Reg:               reg,
		Emitter:           healthEmitter,
		Segment:           &health.SegmentProber{HTTP: probeClient, SoftRetries: softRetries, ProxyPublicBase: cfg.ProxyBaseURL, ProxyInternalBase: cfg.ProxyInternalURL},
		FFProbe:           &health.FFProbe{Path: cfg.HealthFFProbePath(), Timeout: cfg.HealthL2Timeout(), SoftRetries: softRetries, ProxyPublicBase: cfg.ProxyBaseURL, ProxyInternalBase: cfg.ProxyInternalURL},
		L2Every:           cfg.HealthL1Interval(),
		L3Every:           cfg.HealthL2Interval(),
		L3On:              cfg.HealthL2Enabled(),
		L2Workers:         cfg.HealthL1Workers(),
		L3Workers:         cfg.HealthL2Workers(),
		L3HealthySample:   cfg.HealthL2HealthySample(),
		Hosts:             health.NewHostLimiter(cfg.HealthMaxPerHost()),
		StatePath:         filepath.Join(cfg.DataDir, "channelattr", "health_schedule.json"),
		ProxyPublicBase:   cfg.ProxyBaseURL,
		ProxyInternalBase: cfg.ProxyInternalURL,
	}
	go sched.Run(ctx)
	if cfg.HealthL2Enabled() {
		if err := health.EnsureFFProbe(cfg.HealthFFProbePath()); err != nil {
			slog.Warn("ffprobe unavailable; Health L2 probes will fail until fixed", "path", cfg.HealthFFProbePath(), "err", err)
		}
	}

	opsSched := &opsreport.Scheduler{
		Store:   store,
		Reg:     reg,
		Attrs:   attrs,
		DataDir: cfg.DataDir,
		Mailer:  &opsreport.Mailer{},
	}
	svc.SetRefreshTally(opsSched)
	go opsSched.Run(ctx)

	if err := agg.Rebuild(); err != nil && !errors.Is(err, aggregate.ErrEmptyAggregate) {
		slog.Warn("initial aggregate rebuild failed", "err", err)
	}
	go agg.Run(ctx)
	go svc.Run(ctx)
	go svc.WarmLogos(ctx)

	// Post-save reload registry: every UI config save kicks these in order.
	// Restart-only settings (listen/PORT, data_dir) intentionally have no
	// reloader — they are file-only edits that take effect on the next boot.
	store.Register("logging", config.ReloaderFunc(func(_ context.Context, cfg *config.Config) error {
		run.SetLogLevel(cfg.Logging.Level)
		return nil
	}))
	store.Register("health", sched)
	store.Register("ops_report", opsSched)
	store.Register("refresh", svc)

	uiHandler := ui.Handler()
	srv := &server.Server{
		Addr:    cfg.Listen,
		Healthz: server.HealthzHandler(reg),
		Routes: func(mux *http.ServeMux) {
			mux.HandleFunc("GET /api/status", server.StatusHandler(bootStatus))
			mux.HandleFunc("GET /api/cache", server.CacheInventoryHandler(cc, attrs))
			mux.HandleFunc("POST /api/cache/purge", server.CachePurgeAllHandler(svc, ctx))
			mux.HandleFunc("GET /api/config", server.ConfigHandler(store, reg, sched))
			mux.HandleFunc("PUT /api/config", server.ConfigSaveHandler(store, reg, sched))
			mux.HandleFunc("GET /api/groups", server.GroupsHandler(store, svc.GroupsPolicy, reg))
			mux.HandleFunc("PUT /api/groups", server.GroupsSaveHandler(store, svc.GroupsPolicy, reg))
			mux.HandleFunc("GET /api/categories", server.CategoriesHandler(store, svc.CategoriesPolicy, reg))
			mux.HandleFunc("PUT /api/categories", server.CategoriesSaveHandler(store, svc.CategoriesPolicy, reg))
			mux.HandleFunc("GET /api/dedupes", server.DedupesHandler(store, reg))
			mux.HandleFunc("PUT /api/dedupes/apply", server.DedupesApplyHandler(store, reg))
			mux.HandleFunc("GET /api/health/schedule", server.HealthScheduleHandler(sched))
			mux.HandleFunc("GET /api/ops-report/schedule", server.OpsReportScheduleHandler(opsSched))
			mux.HandleFunc("GET /api/ops-report/archives", server.OpsReportArchivesHandler(opsSched))
			mux.HandleFunc("GET /api/ops-report/archives/{id}", server.OpsReportArchiveHandler(opsSched))
			mux.HandleFunc("POST /api/ops-report/archives/{id}/resend", server.OpsReportResendHandler(opsSched))
			mux.HandleFunc("POST /api/ops-report/test-smtp", server.OpsReportTestSMTPHandler(opsSched))
			mux.HandleFunc("POST /api/ops-report/send-preview", server.OpsReportSendPreviewHandler(opsSched))
			mux.HandleFunc("GET /api/providers", server.ProvidersHandler(reg))
			mux.HandleFunc("GET /api/providers/{id}", server.ProviderDetailHandler(reg))
			mux.HandleFunc("POST /api/providers/{id}/refresh", server.ProviderRefreshHandler(svc, ctx))
			mux.HandleFunc("POST /api/providers/{id}/cache/purge", server.ProviderCachePurgeHandler(svc, ctx))
			mux.HandleFunc("GET /api/channels", server.ChannelsHandler(reg, attrs))
			mux.HandleFunc("GET /api/channels/hosts", server.ChannelHostsHandler(reg))
			mux.HandleFunc("GET /api/channels/{provider}/{normalizedId}", server.ChannelHandler(reg, attrs))
			mux.HandleFunc("GET /api/channels/{provider}/{normalizedId}/emit", server.ChannelEmitHandler(store, reg, attrs))
			mux.HandleFunc("PUT /api/channels/{provider}/{normalizedId}/emit", server.ChannelEmitSaveHandler(store, reg))
			mux.HandleFunc("GET /api/channels/{provider}/{normalizedId}/health/history", server.ChannelHealthHistoryHandler(reg, attrs))
			mux.HandleFunc("POST /api/channels/{provider}/{normalizedId}/health/probe", server.ChannelHealthProbeHandler(reg, sched))
			mux.HandleFunc("POST /api/channels/{provider}/{normalizedId}/health/probe/l1", server.ChannelHealthProbeL1Handler(reg, sched))
			mux.HandleFunc("POST /api/channels/{provider}/{normalizedId}/health/probe/l2", server.ChannelHealthProbeL2Handler(reg, sched))
			mux.HandleFunc("GET /api/channels/{provider}/{normalizedId}/programmes", server.ChannelProgrammesHandler(reg))
			mux.HandleFunc("GET /api/channels/{provider}/{normalizedId}/presence/history", server.ChannelPresenceHistoryHandler(reg, attrs))
			mux.HandleFunc("GET /api/presence/summary", server.PresenceSummaryHandler(attrs))
			mux.HandleFunc("GET /api/guide.xml", server.GuideXML(reg))
			mux.HandleFunc("GET /api/guide/{file}", server.GuideProviderXML(reg))
			mux.HandleFunc("GET /metrics", server.MetricsHandler(reg))
			mux.HandleFunc("GET /api/client-access", server.ClientAccessHandler(access))
			mux.HandleFunc("GET /api/proxy/origin/{provider}/{normalizedId}", server.ProxyOriginHandler(reg))
			mux.HandleFunc("POST /api/proxy/events", server.ProxyEventsHandler(proxyAct, healthEmitter, reg))
			mux.HandleFunc("GET /api/proxy/status", server.ProxyStatusHandler(proxyAct))
			mux.HandleFunc("GET /api/proxy/events", server.ProxyEventsQueryHandler(proxyAct))
			mux.HandleFunc("DELETE /api/logos", server.LogosClearHandler(svc, ctx))
			mux.HandleFunc("DELETE /api/logos/{provider}", server.LogosClearHandler(svc, ctx))
			mux.HandleFunc("DELETE /api/logos/{provider}/{channelId}", server.LogosClearHandler(svc, ctx))
			mux.HandleFunc("GET /logos/{provider}/{file}", server.LogoFile(cc))
			mux.HandleFunc("GET /playlist.m3u", server.AggregatePlaylist(cc, access))
			mux.HandleFunc("GET /epg.xml", server.AggregateGuide(cc, access))
			mux.HandleFunc("GET /{file}", server.PlaylistFile(reg, cc, uiHandler, access))
			mux.Handle("/", uiHandler)
		},
	}
	if err := srv.Run(ctx); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
	slog.Info("shutting down")
}

// configPath resolves the config.yaml location (FASTGEN_CONFIG or the default).
func configPath() string {
	if p := os.Getenv("FASTGEN_CONFIG"); p != "" {
		return p
	}
	return config.DefaultPath
}
