package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/proxy"
	"github.com/j27-aurum/gofast/internal/run"
	"github.com/j27-aurum/gofast/internal/server"
)

func main() {
	run.SetupLogger("fastproxy")
	ctx, stop := run.SignalContext(context.Background())
	defer stop()

	addr := config.ListenFromEnv(":8181")
	genURL := strings.TrimRight(strings.TrimSpace(os.Getenv("FASTPROXY_GEN_URL")), "/")
	if genURL == "" {
		slog.Error("FASTPROXY_GEN_URL is required (internal FASTGen origin, e.g. http://fastgen:8180)")
		os.Exit(1)
	}

	store := proxy.NewStore()
	origin := proxy.NewGenClient(genURL, &http.Client{Timeout: 15 * time.Second})
	reporter := proxy.NewReporter(genURL, store)
	go reporter.Run(ctx)

	h := proxy.NewHandler(origin, store, reporter)
	slog.Info("starting", "listen", addr, "gen_url", genURL)

	srv := &server.Server{
		Addr: addr,
		Routes: func(mux *http.ServeMux) {
			h.Register(mux)
		},
	}
	if err := srv.Run(ctx); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
	slog.Info("shutting down")
}
