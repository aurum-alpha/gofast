package server

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/health"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

// ConfigSource describes where the running config was loaded from.
type ConfigSource struct {
	Path     string `json:"path"`
	FromFile bool   `json:"from_file"`
}

// ConfigHealth is the effective health probe / FSM settings.
type ConfigHealth struct {
	ConsecutiveFailures int     `json:"consecutive_failures"`
	ExcludeUnhealthy    bool    `json:"exclude_unhealthy"`
	L2Interval          string  `json:"l2_interval"`
	L2Workers           int     `json:"l2_workers"`
	L3Enabled           bool    `json:"l3_enabled"`
	L3Interval          string  `json:"l3_interval"`
	L3Workers           int     `json:"l3_workers"`
	L3Timeout           string  `json:"l3_timeout"`
	L3HealthySample     float64 `json:"l3_healthy_sample"`
	MaxPerHost          int     `json:"max_per_host"`
	SoftRetries         int     `json:"soft_retries"`
	FFProbePath         string  `json:"ffprobe_path"`
}

// ConfigArtworkTLS is a redacted per-host logo TLS policy (CA PEM not exposed).
type ConfigArtworkTLS struct {
	Host               string `json:"host"`
	CAPemSet           bool   `json:"ca_pem_set"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify"`
}

// ConfigResponse is GET /api/config — effective deploy + health + provider settings.
type ConfigResponse struct {
	Source       ConfigSource             `json:"source"`
	Listen       string                   `json:"listen"`
	BaseURL      string                   `json:"base_url"`
	DataDir      string                   `json:"data_dir"`
	ProxyBaseURL string                   `json:"proxy_base_url"`
	ProxyAll     bool                     `json:"proxy_all"`
	CacheLogos   bool                     `json:"cache_logos"`
	HTTPTimeout  string                   `json:"http_client_timeout"`
	LogLevel     string                   `json:"log_level"`
	Health       ConfigHealth             `json:"health"`
	Schedule     *health.Schedule         `json:"probe_schedule,omitempty"`
	ArtworkTLS   []ConfigArtworkTLS       `json:"artwork_tls"`
	Providers    []model.ProviderSettings `json:"providers"`
}

// ConfigHandler serves GET /api/config (read-only effective settings).
func ConfigHandler(cfg *config.Config, path string, fromFile bool, reg *provider.Registry, sched *health.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if cfg == nil {
			http.Error(w, "config unavailable", http.StatusServiceUnavailable)
			return
		}
		out := ConfigResponse{
			Source:       ConfigSource{Path: path, FromFile: fromFile},
			Listen:       cfg.Listen,
			BaseURL:      cfg.BaseURL,
			DataDir:      cfg.DataDir,
			ProxyBaseURL: cfg.ProxyBaseURL,
			ProxyAll:     cfg.ProxyAllEnabled(),
			CacheLogos:   cfg.CacheLogosEnabled(),
			HTTPTimeout:  cfg.Timeouts.HTTPClient.String(),
			LogLevel:     cfg.Logging.Level,
			Health: ConfigHealth{
				ConsecutiveFailures: cfg.HealthConsecutiveFailures(),
				ExcludeUnhealthy:    cfg.HealthExcludeUnhealthy(),
				L2Interval:          cfg.HealthL2Interval().String(),
				L2Workers:           cfg.HealthL2Workers(),
				L3Enabled:           cfg.HealthL3Enabled(),
				L3Interval:          cfg.HealthL3Interval().String(),
				L3Workers:           cfg.HealthL3Workers(),
				L3Timeout:           cfg.HealthL3Timeout().String(),
				L3HealthySample:     cfg.HealthL3HealthySample(),
				MaxPerHost:          cfg.HealthMaxPerHost(),
				SoftRetries:         cfg.HealthSoftRetries(),
				FFProbePath:         cfg.HealthFFProbePath(),
			},
			ArtworkTLS: artworkTLSView(cfg),
		}
		if sched != nil {
			snap := sched.Snapshot()
			out.Schedule = &snap
		}
		if reg != nil {
			out.Providers = reg.Providers().Providers
		} else {
			out.Providers = []model.ProviderSettings{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}

func artworkTLSView(cfg *config.Config) []ConfigArtworkTLS {
	if cfg == nil || len(cfg.ArtworkTLS) == 0 {
		return []ConfigArtworkTLS{}
	}
	hosts := make([]string, 0, len(cfg.ArtworkTLS))
	for host := range cfg.ArtworkTLS {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	out := make([]ConfigArtworkTLS, 0, len(hosts))
	for _, host := range hosts {
		p := cfg.ArtworkTLS[host]
		out = append(out, ConfigArtworkTLS{
			Host:               host,
			CAPemSet:           p.CAPem != "",
			InsecureSkipVerify: p.InsecureSkipVerify,
		})
	}
	return out
}
