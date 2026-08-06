package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/health"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/providerset"
)

// restartOnly are config paths that have no live reloader by design: editing
// them means "edit config.yaml and restart". The UI shows them read-only and
// the PUT endpoint rejects ops that target them.
var restartOnly = map[string]bool{
	"listen":   true,
	"data_dir": true,
}

// ConfigSource describes where the running config was loaded from.
type ConfigSource struct {
	Path     string `json:"path"`
	FromFile bool   `json:"from_file"`
	Writable bool   `json:"writable"`
}

// ConfigField is one editable (or locked) setting: its effective value, where
// it came from, and whether a UI save would take effect.
type ConfigField struct {
	Value           any    `json:"value"`
	Source          string `json:"source"` // default | file | env
	Editable        bool   `json:"editable"`
	Env             string `json:"env,omitempty"`
	RestartRequired bool   `json:"restart_required,omitempty"`
}

// ConfigProvider is one shipped provider in the catalog: its effective
// settings plus which optional fields its adapter actually reads.
type ConfigProvider struct {
	Settings     model.ProviderSettings `json:"settings"`
	Configured   bool                   `json:"configured"` // has a providers.<id> block
	FieldSupport []string               `json:"field_support"`
}

// ConfigArtworkTLS is a redacted per-host logo TLS policy (CA PEM not exposed).
type ConfigArtworkTLS struct {
	Host               string `json:"host"`
	CAPemSet           bool   `json:"ca_pem_set"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify"`
}

// ConfigResponse is GET /api/config.
type ConfigResponse struct {
	Revision   string                 `json:"revision"`
	Source     ConfigSource           `json:"source"`
	Fields     map[string]ConfigField `json:"fields"`
	Schedule   *health.Schedule       `json:"probe_schedule,omitempty"`
	ArtworkTLS []ConfigArtworkTLS     `json:"artwork_tls"`
	Providers  []ConfigProvider       `json:"providers"`
}

// configSaveRequest is the PUT /api/config body: typed path ops plus the
// revision the client loaded (optimistic concurrency).
type configSaveRequest struct {
	Revision string          `json:"revision"`
	Ops      []config.PathOp `json:"ops"`
}

// configSaveResponse reports the new revision and each subsystem's reload outcome.
type configSaveResponse struct {
	Revision string                `json:"revision"`
	Reloads  []config.ReloadResult `json:"reloads"`
}

// ConfigHandler serves GET /api/config: the full settings surface with per-field
// provenance so the UI can render typed controls and lock env-shadowed fields.
// Secrets never appear (artwork CA PEMs are redacted; no other secret exists).
func ConfigHandler(store *config.Store, reg *provider.Registry, sched *health.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		cfg := store.Current()
		if cfg == nil {
			http.Error(w, "config unavailable", http.StatusServiceUnavailable)
			return
		}
		fileKeys, err := config.FileKeys(store.Path())
		if err != nil {
			fileKeys = map[string]bool{}
		}
		writable := config.ProbeWritable(store.Path()) == nil
		out := ConfigResponse{
			Revision: store.Revision(),
			Source: ConfigSource{
				Path:     store.Path(),
				FromFile: store.FromFile(),
				Writable: writable,
			},
			Fields:     configFields(cfg, fileKeys, writable),
			ArtworkTLS: artworkTLSView(cfg),
			Providers:  configProviders(cfg),
		}
		if sched != nil {
			snap := sched.Snapshot()
			out.Schedule = &snap
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}

// ConfigSaveHandler serves PUT /api/config: typed path ops + revision through
// Store.Save (validate via the boot load path, persist comment-preserving,
// reload the snapshot, kick every Reloader). Errors: 400 malformed, 403
// read-only, 409 stale revision, 422 invalid candidate or locked field.
func ConfigSaveHandler(store *config.Store, reg *provider.Registry, sched *health.Scheduler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !sameOrigin(r) {
			http.Error(w, "cross-origin request rejected", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var req configSaveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if len(req.Ops) == 0 {
			http.Error(w, "no ops", http.StatusBadRequest)
			return
		}
		shadow := config.EnvShadow()
		for _, op := range req.Ops {
			if op.Path == "ops_report.smtp.password_set" {
				http.Error(w, "ops_report.smtp.password_set is read-only", http.StatusUnprocessableEntity)
				return
			}
			if restartOnly[op.Path] {
				http.Error(w, op.Path+" requires editing config.yaml and restarting; it is not live-editable", http.StatusUnprocessableEntity)
				return
			}
			if env, locked := shadow[op.Path]; locked {
				http.Error(w, op.Path+" is set by "+env+"; unset the environment variable to edit it", http.StatusUnprocessableEntity)
				return
			}
		}
		reloads, err := store.Save(r.Context(), req.Revision, req.Ops)
		if err != nil {
			switch {
			case errors.Is(err, config.ErrStaleRevision):
				http.Error(w, "config changed since it was loaded; reload and retry", http.StatusConflict)
			case errors.Is(err, config.ErrReadOnly):
				http.Error(w, "config file is read-only; mount /data (or the config path) read-write to save settings", http.StatusForbidden)
			case errors.Is(err, config.ErrInvalid):
				http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			default:
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(configSaveResponse{
			Revision: store.Revision(),
			Reloads:  reloads,
		})
	}
}

// configFields builds the per-field view for every top-level setting the UI
// renders. Provider settings are reported separately (configProviders).
func configFields(cfg *config.Config, fileKeys map[string]bool, writable bool) map[string]ConfigField {
	shadow := config.EnvShadow()
	out := map[string]ConfigField{}
	add := func(path string, value any) {
		f := ConfigField{Value: value, Source: "default", RestartRequired: restartOnly[path]}
		if fileKeys[path] {
			f.Source = "file"
		}
		if env, ok := shadow[path]; ok {
			f.Source = "env"
			f.Env = env
		}
		f.Editable = writable && f.Source != "env" && !f.RestartRequired
		out[path] = f
	}
	add("listen", cfg.Listen)
	add("data_dir", cfg.DataDir)
	add("base_url", cfg.BaseURL)
	add("proxy_base_url", cfg.ProxyBaseURL)
	add("proxy_internal_url", cfg.ProxyInternalURL)
	add("proxy_all", cfg.ProxyAllEnabled())
	add("cache_logos", cfg.CacheLogosEnabled())
	add("regions", cfg.EffectiveRegions())
	add("timeouts.http_client", cfg.Timeouts.HTTPClient.String())
	add("logging.level", cfg.Logging.Level)
	add("health.consecutive_failures", cfg.HealthConsecutiveFailures())
	add("health.exclude_unhealthy", cfg.HealthExcludeUnhealthy())
	add("health.l1_interval", cfg.HealthL1Interval().String())
	add("health.l1_workers", cfg.HealthL1Workers())
	add("health.l2_enabled", cfg.HealthL2Enabled())
	add("health.l2_interval", cfg.HealthL2Interval().String())
	add("health.l2_workers", cfg.HealthL2Workers())
	add("health.l2_timeout", cfg.HealthL2Timeout().String())
	add("health.l2_healthy_sample", cfg.HealthL2HealthySample())
	add("health.max_per_host", cfg.HealthMaxPerHost())
	add("health.soft_retries", cfg.HealthSoftRetries())
	add("health.ffprobe_path", cfg.HealthFFProbePath())
	add("ops_report.enabled", cfg.OpsReport.IsEnabled())
	add("ops_report.timezone", cfg.OpsReport.TimezoneOrDefault())
	add("ops_report.send_at", cfg.OpsReport.SendAtOrDefault())
	add("ops_report.from", cfg.OpsReport.From)
	add("ops_report.to", cfg.OpsReport.To)
	add("ops_report.smtp.host", cfg.OpsReport.SMTP.Host)
	add("ops_report.smtp.port", cfg.OpsReport.SMTP.PortOrDefault())
	add("ops_report.smtp.starttls", cfg.OpsReport.SMTP.STARTTLSOrDefault())
	add("ops_report.smtp.username", cfg.OpsReport.SMTP.Username)
	// Never echo the SMTP password; expose set/not-set only.
	pw := ConfigField{Value: "", Source: "default"}
	if fileKeys["ops_report.smtp.password"] {
		pw.Source = "file"
	}
	if env, ok := shadow["ops_report.smtp.password"]; ok {
		pw.Source = "env"
		pw.Env = env
	}
	pw.Editable = writable && pw.Source != "env"
	out["ops_report.smtp.password"] = pw
	out["ops_report.smtp.password_set"] = ConfigField{
		Value:    cfg.OpsReport.PasswordSet(),
		Source:   "default",
		Editable: false,
	}
	return out
}

// configProviders returns the shipped-provider catalog with effective settings
// and per-adapter field support, sorted by id.
func configProviders(cfg *config.Config) []ConfigProvider {
	effective := providerset.Settings(cfg.Providers, cfg.EffectiveRegions())
	out := make([]ConfigProvider, 0, len(effective))
	for _, id := range providerset.Known() {
		s := effective[id]
		s.ID = id
		_, configured := cfg.Providers[id]
		fields := providerset.FieldSupport(id)
		if fields == nil {
			fields = []string{}
		}
		out = append(out, ConfigProvider{
			Settings:     s,
			Configured:   configured,
			FieldSupport: fields,
		})
	}
	return out
}

// sameOrigin accepts requests without an Origin header (curl, same-origin GET
// navigations) and browser requests whose Origin host matches the request host.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" || origin == "null" {
		return origin == ""
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
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
