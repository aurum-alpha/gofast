package config

import (
	"log/slog"
	"testing"

	"github.com/j27-aurum/gofast/internal/model"
)

func TestLogLoadedNoProviders(t *testing.T) {
	// Ensure LogLoaded does not panic with empty providers (emits a warn).
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	slog.SetDefault(slog.New(slog.DiscardHandler))

	cfg := defaults()
	cfg.LogLoaded("/data/config.yaml", false)
}

func TestLogLoadedWithProviders(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	slog.SetDefault(slog.New(slog.DiscardHandler))

	cfg := &Config{
		Listen:  DefaultListen,
		DataDir: DefaultDataDir,
		Providers: map[string]model.ProviderSettings{
			"lg": {Label: "LG", MinChannels: 50, RefreshInterval: 0},
		},
	}
	cfg.LogLoaded("/data/config.yaml", true)
}
