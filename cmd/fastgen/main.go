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
	"github.com/j27-aurum/gofast/internal/classifier"
	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/provider/lg"
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

	// Providers are code: each known provider's package defaults are overlaid by
	// its YAML settings. Unknown YAML ids have no implementation (warned).
	settings := map[model.ProviderID]model.ProviderSettings{
		model.ProviderLG: lg.DefaultSettings().Merge(cfg.Providers[model.ProviderLG]),
	}
	for id := range cfg.Providers {
		if _, known := settings[id]; !known {
			slog.Warn("provider in config has no implementation; ignoring", "id", id)
		}
	}
	readers := map[model.ProviderID]provider.Reader{}
	if s := settings[model.ProviderLG]; s.IsEnabled() {
		readers[model.ProviderLG] = lg.New(s, client)
	} else {
		slog.Info("provider disabled", "id", model.ProviderLG)
	}
	reg := provider.NewRegistry(readers, settings)
	reg.LogLoaded()

	// Warm feeds from disk, regenerate the aggregate for consistency, then start
	// the aggregator and the per-provider refresh scheduler.
	refresh.Restore(reg, cc)
	agg := aggregate.New(reg, cc)
	if err := agg.Rebuild(); err != nil {
		slog.Warn("initial aggregate rebuild failed", "err", err)
	}
	go agg.Run(ctx)

	clf := classifier.New(client, 0)
	svc := refresh.New(reg, clf, cc, agg.Notify)
	go svc.Run(ctx)

	uiHandler := ui.Handler()
	srv := &server.Server{
		Addr: cfg.Listen,
		Routes: func(mux *http.ServeMux) {
			mux.HandleFunc("GET /api/providers", server.ProvidersHandler(reg))
			mux.HandleFunc("GET /api/providers/{id}", server.ProviderDetailHandler(reg))
			mux.HandleFunc("GET /api/channels", server.ChannelsHandler(reg))
			mux.HandleFunc("GET /api/guide.xml", server.GuideXML(reg))
			mux.HandleFunc("GET /api/guide/{file}", server.GuideProviderXML(reg))
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
