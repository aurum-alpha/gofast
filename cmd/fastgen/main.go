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
	config.LogLoaded(path, fromFile, cfg)

	stub := &server.Stub{
		Addr: cfg.Listen,
		Routes: func(mux *http.ServeMux) {
			mux.HandleFunc("GET /api/providers", server.ProvidersHandler(path, fromFile, cfg))
			mux.Handle("/", ui.Handler())
		},
	}
	if err := stub.Run(ctx); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
	slog.Info("shutting down")
}

func loadConfig() (cfg config.Config, path string, fromFile bool, err error) {
	path = os.Getenv("FASTGEN_CONFIG")
	if path == "" {
		path = config.DefaultPath
	}
	cfg, err = config.Load(path)
	if err == nil {
		return cfg, path, true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		slog.Warn("config file missing; using defaults and environment", "path", path)
		cfg = config.Defaults()
		config.ApplyEnv(&cfg)
		return cfg, path, false, nil
	}
	return config.Config{}, path, false, err
}
