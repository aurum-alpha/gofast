package main

import (
	"context"
	"log/slog"

	"github.com/j27-aurum/gofast/internal/run"
)

func main() {
	run.SetupLogger("fastgen")
	ctx, stop := run.SignalContext(context.Background())
	defer stop()

	slog.Info("starting")
	<-ctx.Done()
	slog.Info("shutting down")
}
