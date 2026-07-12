package run

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

// SetupLogger configures the default slog logger for CLI binaries.
func SetupLogger(service string) {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})).With("service", service))
}

// SignalContext returns a context cancelled on SIGINT or SIGTERM.
func SignalContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
		signal.Stop(sigCh)
	}()
	return ctx, cancel
}
