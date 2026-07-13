package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"

	"github.com/j27-aurum/gofast/internal/config"
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

	srv := &server.Server{
		Addr: cfg.Listen,
		Routes: func(mux *http.ServeMux) {
			mux.HandleFunc("GET /api/providers", server.ProvidersHandler(cfg))
			mux.Handle("/", ui.Handler())
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
