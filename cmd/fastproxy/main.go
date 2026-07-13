package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/run"
	"github.com/j27-aurum/gofast/internal/server"
)

func main() {
	run.SetupLogger("fastproxy")
	ctx, stop := run.SignalContext(context.Background())
	defer stop()

	addr := config.ListenFromEnv(":8181")
	slog.Info("starting", "listen", addr)

	srv := &server.Server{Addr: addr}
	if err := srv.Run(ctx); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
	slog.Info("shutting down")
}
