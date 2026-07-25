package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/version"
)

// healthzResponse is the GET /healthz JSON envelope.
type healthzResponse struct {
	OK        bool              `json:"ok"`
	Version   version.Info      `json:"version"`
	Providers []healthzProvider `json:"providers,omitempty"`
}

// healthzProvider is one enabled feed's ops status for /healthz.
type healthzProvider struct {
	ID                            string    `json:"id"`
	Label                         string    `json:"label,omitempty"`
	Stale                         bool      `json:"stale"`
	FetchedAt                     time.Time `json:"fetched_at,omitempty"`
	LastAttemptAt                 time.Time `json:"last_attempt_at,omitempty"`
	LastError                     string    `json:"last_error,omitempty"`
	LastErrorAt                   time.Time `json:"last_error_at,omitempty"`
	ExportedChannels              int       `json:"exported_channels"`
	ExportedProgrammes            int       `json:"exported_programmes"`
	GuideHoursAhead               float64   `json:"guide_hours_ahead"`
	RefreshIntervalClamped        bool      `json:"refresh_interval_clamped"`
	RefreshIntervalConfigured     string    `json:"refresh_interval_configured,omitempty"`
	RefreshIntervalEffective      string    `json:"refresh_interval_effective,omitempty"`
	RefreshIntervalConfiguredSecs float64   `json:"refresh_interval_configured_seconds,omitempty"`
	RefreshIntervalEffectiveSecs  float64   `json:"refresh_interval_effective_seconds,omitempty"`
}

// HealthzHandler serves GET /healthz. With a nil registry (fastproxy / tests),
// it returns the process-up stub {"ok":true}. With a registry, it includes
// per-provider refresh status; HTTP is always 200 while the process is up —
// staleness is per-provider, not process-down.
func HealthzHandler(reg *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if reg == nil {
			_ = json.NewEncoder(w).Encode(healthzResponse{OK: true, Version: version.Current()})
			return
		}
		feeds := reg.Feeds()
		out := healthzResponse{
			OK:        true,
			Version:   version.Current(),
			Providers: make([]healthzProvider, 0, len(feeds)),
		}
		for _, feed := range feeds {
			stats := feed.Stats()
			configured, effective, clamped := feed.RefreshSchedule()
			out.Providers = append(out.Providers, healthzProvider{
				ID:                            string(feed.ID()),
				Label:                         feed.Label(),
				Stale:                         stats.LastError != "",
				FetchedAt:                     stats.FetchedAt,
				LastAttemptAt:                 stats.LastAttemptAt,
				LastError:                     stats.LastError,
				LastErrorAt:                   stats.LastErrorAt,
				ExportedChannels:              stats.ExportedChannels,
				ExportedProgrammes:            stats.ExportedProgrammes,
				GuideHoursAhead:               stats.GuideHoursAhead,
				RefreshIntervalClamped:        clamped,
				RefreshIntervalConfigured:     stats.RefreshIntervalConfigured,
				RefreshIntervalEffective:      stats.RefreshIntervalEffective,
				RefreshIntervalConfiguredSecs: configured.Seconds(),
				RefreshIntervalEffectiveSecs:  effective.Seconds(),
			})
		}
		_ = json.NewEncoder(w).Encode(out)
	}
}
