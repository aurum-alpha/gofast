package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"

	"github.com/j27-aurum/gofast/internal/classifier"
	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/provider/lg"
	"github.com/j27-aurum/gofast/internal/refresh"
	"github.com/j27-aurum/gofast/internal/run"
	"github.com/j27-aurum/gofast/internal/server"
	"github.com/j27-aurum/gofast/internal/snapshot"
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

	// Providers are code. Each known provider's package defaults are overlaid by
	// its YAML settings; the map literal below wires the enabled ones. YAML ids
	// with no implementation are ignored (warned).
	settings := map[string]model.ProviderSettings{
		"lg": lg.DefaultSettings().Merge(cfg.Providers["lg"]),
	}
	for id := range cfg.Providers {
		if _, known := settings[id]; !known {
			slog.Warn("provider in config has no implementation; ignoring", "id", id)
		}
	}
	readers := map[string]provider.Reader{}
	if s := settings["lg"]; s.IsEnabled() {
		readers["lg"] = lg.New(s, client)
	} else {
		slog.Info("provider disabled", "id", "lg")
	}
	reg := provider.NewRegistry(readers, settings)
	reg.LogLoaded()

	store := snapshot.NewStore()
	clf := classifier.New(client, 0)
	refresh.Once(ctx, reg, store, clf)
	go refresh.Loop(ctx, reg, store, clf)

	uiHandler := ui.Handler()
	srv := &server.Server{
		Addr: cfg.Listen,
		Routes: func(mux *http.ServeMux) {
			mux.HandleFunc("GET /api/providers", server.ProvidersHandler(settings))
			mux.HandleFunc("GET /api/channels", server.ChannelsHandler(store))
			mux.HandleFunc("GET /api/guide.xml", server.GuideXML(store))
			mux.HandleFunc("GET /api/guide/{file}", server.GuideProviderXML(store))
			mux.HandleFunc("GET /{file}", server.PlaylistFile(store, uiHandler))
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
