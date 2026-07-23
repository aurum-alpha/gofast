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
	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/logocache"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/provider/distrotv"
	"github.com/j27-aurum/gofast/internal/provider/lg"
	"github.com/j27-aurum/gofast/internal/provider/localnow"
	"github.com/j27-aurum/gofast/internal/provider/pluto"
	"github.com/j27-aurum/gofast/internal/provider/roku"
	"github.com/j27-aurum/gofast/internal/provider/samsung"
	"github.com/j27-aurum/gofast/internal/provider/xumo"
	"github.com/j27-aurum/gofast/internal/refresh"
	"github.com/j27-aurum/gofast/internal/run"
	"github.com/j27-aurum/gofast/internal/server"
	"github.com/j27-aurum/gofast/internal/ui"
)

func main() {
	run.SetupLogger("fastgen")
	ctx, stop := run.SignalContext(context.Background())
	defer stop()

	cfg, path, fromFile, err := loadConfig()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	cfg.LogLoaded(path, fromFile)

	client := httpx.NewClient(cfg.Timeouts.HTTPClient, 0)
	cc := cache.New(filepath.Join(cfg.DataDir, "cache"))

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

	var logos *logocache.Cache
	if cfg.CacheLogosEnabled() {
		artworkHosts := make(map[string]logocache.HostPolicy, len(cfg.ArtworkTLS))
		for host, policy := range cfg.ArtworkTLS {
			artworkHosts[host] = logocache.HostPolicy{
				CAPem:              policy.CAPem,
				InsecureSkipVerify: policy.InsecureSkipVerify,
			}
		}
		artworkClient, err := logocache.NewArtworkClient(cfg.Timeouts.HTTPClient, artworkHosts)
		if err != nil {
			slog.Error("artwork http client", "err", err)
			os.Exit(1)
		}
		logos = logocache.New(cc, artworkClient, cfg.BaseURL, 0)
	}

	// Providers are code: each known provider's package defaults are overlaid by
	// its YAML settings. Unknown YAML ids have no implementation (warned).
	settings := knownProviderSettings(cfg.Providers)
	for id := range cfg.Providers {
		if _, known := settings[id]; !known {
			slog.Warn("provider in config has no implementation; ignoring", "id", id)
		}
	}
	readers := knownProviderReaders(settings, client)
	reg := provider.NewRegistry(readers, settings)
	reg.LogLoaded()

	// Warm feeds from disk (may seed legacy meta classifications into attrs),
	// regenerate the aggregate, then start AttrReceiver + refresh.
	emissionPolicy := refresh.EmissionPolicy{
		ProxyBaseURL: cfg.ProxyBaseURL,
		ProxyAll:     cfg.ProxyAllEnabled(),
	}
	bootStatus := &refresh.Status{}
	refresh.Restore(reg, cc, emissionPolicy, attrs)

	attrBus := channelattr.NewBus(256)
	go channelattr.Receive(ctx, attrBus, attrs)

	agg := aggregate.New(reg, cc)
	if err := agg.Rebuild(); err != nil && !errors.Is(err, aggregate.ErrEmptyAggregate) {
		slog.Warn("initial aggregate rebuild failed", "err", err)
	}
	go agg.Run(ctx)

	clf := classifier.New(client, 0)
	svc := refresh.New(reg, clf, cc, emissionPolicy, logos, attrs, attrBus, agg.Notify, bootStatus)
	go svc.Run(ctx)
	go refresh.WarmLogos(ctx, reg, cc, emissionPolicy, logos, agg.Notify, bootStatus)

	uiHandler := ui.Handler()
	srv := &server.Server{
		Addr:    cfg.Listen,
		Healthz: server.HealthzHandler(reg),
		Routes: func(mux *http.ServeMux) {
			mux.HandleFunc("GET /api/status", server.StatusHandler(bootStatus))
			mux.HandleFunc("GET /api/providers", server.ProvidersHandler(reg))
			mux.HandleFunc("GET /api/providers/{id}", server.ProviderDetailHandler(reg))
			mux.HandleFunc("GET /api/channels", server.ChannelsHandler(reg))
			mux.HandleFunc("GET /api/channels/{provider}/{normalizedId}", server.ChannelHandler(reg))
			mux.HandleFunc("GET /api/guide.xml", server.GuideXML(reg))
			mux.HandleFunc("GET /api/guide/{file}", server.GuideProviderXML(reg))
			mux.HandleFunc("GET /metrics", server.MetricsHandler(reg))
			mux.HandleFunc("GET /logos/{provider}/{file}", server.LogoFile(cc))
			mux.HandleFunc("GET /playlist.m3u", server.AggregatePlaylist(cc))
			mux.HandleFunc("GET /epg.xml", server.AggregateGuide(cc))
			mux.HandleFunc("GET /{file}", server.PlaylistFile(cc, uiHandler))
			mux.Handle("/", uiHandler)
		},
	}
	if err := srv.Run(ctx); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
	slog.Info("shutting down")
}

func knownProviderReaders(settings map[model.ProviderID]model.ProviderSettings, client *httpx.Client) map[model.ProviderID]provider.Reader {
	readers := map[model.ProviderID]provider.Reader{}
	if s := settings[model.ProviderDistroTV]; s.IsEnabled() {
		readers[model.ProviderDistroTV] = distrotv.New(s, client)
	} else {
		slog.Info("provider disabled", "id", model.ProviderDistroTV)
	}
	if s := settings[model.ProviderLG]; s.IsEnabled() {
		readers[model.ProviderLG] = lg.New(s, client)
	} else {
		slog.Info("provider disabled", "id", model.ProviderLG)
	}
	if s := settings[model.ProviderLocalNow]; s.IsEnabled() {
		readers[model.ProviderLocalNow] = localnow.New(s, client)
	} else {
		slog.Info("provider disabled", "id", model.ProviderLocalNow)
	}
	if s := settings[model.ProviderPluto]; s.IsEnabled() {
		readers[model.ProviderPluto] = pluto.New(s, client)
	} else {
		slog.Info("provider disabled", "id", model.ProviderPluto)
	}
	if s := settings[model.ProviderRoku]; s.IsEnabled() {
		readers[model.ProviderRoku] = roku.New(s, client)
	} else {
		slog.Info("provider disabled", "id", model.ProviderRoku)
	}
	if s := settings[model.ProviderSamsung]; s.IsEnabled() {
		readers[model.ProviderSamsung] = samsung.New(s, client)
	} else {
		slog.Info("provider disabled", "id", model.ProviderSamsung)
	}
	if s := settings[model.ProviderXumo]; s.IsEnabled() {
		readers[model.ProviderXumo] = xumo.New(s, client)
	} else {
		slog.Info("provider disabled", "id", model.ProviderXumo)
	}
	return readers
}

func knownProviderSettings(overlays map[model.ProviderID]model.ProviderSettings) map[model.ProviderID]model.ProviderSettings {
	distroTVOverlay, distroTVConfigured := overlays[model.ProviderDistroTV]
	localNowOverlay, localNowConfigured := overlays[model.ProviderLocalNow]
	plutoOverlay, plutoConfigured := overlays[model.ProviderPluto]
	rokuOverlay, rokuConfigured := overlays[model.ProviderRoku]
	samsungOverlay, samsungConfigured := overlays[model.ProviderSamsung]
	xumoOverlay, xumoConfigured := overlays[model.ProviderXumo]
	return map[model.ProviderID]model.ProviderSettings{
		model.ProviderDistroTV: distrotv.DefaultSettings().MergeConfigured(distroTVOverlay, distroTVConfigured),
		model.ProviderLG:       lg.DefaultSettings().Merge(overlays[model.ProviderLG]),
		model.ProviderLocalNow: localnow.DefaultSettings().MergeConfigured(localNowOverlay, localNowConfigured),
		model.ProviderPluto:    pluto.DefaultSettings().MergeConfigured(plutoOverlay, plutoConfigured),
		model.ProviderRoku:     roku.DefaultSettings().MergeConfigured(rokuOverlay, rokuConfigured),
		model.ProviderSamsung:  samsung.DefaultSettings().MergeConfigured(samsungOverlay, samsungConfigured),
		model.ProviderXumo:     xumo.DefaultSettings().MergeConfigured(xumoOverlay, xumoConfigured),
	}
}

func loadConfig() (cfg *config.Config, path string, fromFile bool, err error) {
	path = os.Getenv("FASTGEN_CONFIG")
	if path == "" {
		path = config.DefaultPath
	}
	cfg, err = config.New(path)
	if err == nil {
		return cfg, path, true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		slog.Warn("config file missing; using defaults and environment", "path", path)
		cfg, err = config.New("")
		return cfg, path, false, err
	}
	return nil, path, false, err
}
