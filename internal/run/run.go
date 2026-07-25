package run

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

// logLevel is the live minimum level for the default logger; SetLogLevel
// adjusts it at runtime (config hot reload).
var logLevel slog.LevelVar

// SetupLogger configures the default slog logger for CLI binaries.
func SetupLogger(service string) {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: &logLevel,
	})).With("service", service))
}

// SetLogLevel applies a config logging level ("debug", "info", "warn",
// "error"). Unknown or empty values fall back to info.
func SetLogLevel(level string) {
	var l slog.Level
	if level == "" || l.UnmarshalText([]byte(level)) != nil {
		l = slog.LevelInfo
	}
	logLevel.Set(l)
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
