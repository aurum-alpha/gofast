package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/j27-aurum/gofast/internal/run"
	"github.com/j27-aurum/gofast/internal/server"
)

func main() {
	run.SetupLogger("fastgen")
	ctx, stop := run.SignalContext(context.Background())
	defer stop()

	addr := envOr("FASTGEN_LISTEN", ":8180")
	slog.Info("starting", "listen", addr)

	stub := &server.Stub{Addr: addr}
	if err := stub.Run(ctx); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
	slog.Info("shutting down")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
