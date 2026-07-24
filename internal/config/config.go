// Package config loads deploy and runtime settings for fastgen / fastproxy.
//
// Deploy-varying values follow https://12factor.net/config: environment variables
// are the primary override. An optional YAML file on the data volume may supply
// structured defaults; it is never required when env + code defaults suffice.
package config

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
	"gopkg.in/yaml.v3"
)

const (
	DefaultPath    = "/data/config.yaml"
	DefaultListen  = ":8180"
	DefaultDataDir = "/data"
)

// Config is the configuration surface for fastgen.
type Config struct {
	Listen       string                                      `yaml:"listen"`
	BaseURL      string                                      `yaml:"base_url"`
	DataDir      string                                      `yaml:"data_dir"`
	ProxyBaseURL string                                      `yaml:"proxy_base_url"`
	ProxyAll     *bool                                       `yaml:"proxy_all"`
	CacheLogos   *bool                                       `yaml:"cache_logos"`
	ArtworkTLS   map[string]ArtworkTLS                       `yaml:"artwork_tls"`
	Timeouts     Timeouts                                    `yaml:"timeouts"`
	Logging      Logging                                     `yaml:"logging"`
	Health       Health                                      `yaml:"health"`
	Providers    map[model.ProviderID]model.ProviderSettings `yaml:"providers"`
}

// Health holds channel-health FSM and probe knobs (attr store is separate).
type Health struct {
	// ConsecutiveFailures is N for DOWN (default 3). Zero in YAML means unset.
	ConsecutiveFailures int `yaml:"consecutive_failures"`
	// ExcludeUnhealthy prunes HealthDown channels from export (default false).
	ExcludeUnhealthy *bool `yaml:"exclude_unhealthy"`
	// L2Interval is how often to run NATIVE segment probes (default 24h).
	L2Interval time.Duration `yaml:"l2_interval"`
	// L2Workers bounds concurrent L2 probes (default 4).
	L2Workers int `yaml:"l2_workers"`
	// L3Enabled turns on scheduled ffprobe (default false).
	L3Enabled *bool `yaml:"l3_enabled"`
	// L3Interval is the jitter window for scheduled L3 (default 60m).
	L3Interval time.Duration `yaml:"l3_interval"`
	// L3Workers bounds concurrent L3 probes (default 2).
	L3Workers int `yaml:"l3_workers"`
	// L3Timeout is per-probe ffprobe timeout (default 30s).
	L3Timeout time.Duration `yaml:"l3_timeout"`
	// L3HealthySample is fraction of healthy channels probed each L3 sweep (default 0.1).
	L3HealthySample *float64 `yaml:"l3_healthy_sample"`
	// MaxPerHost caps concurrent probes per CDN hostname (default 2).
	MaxPerHost int `yaml:"max_per_host"`
	// SoftRetries is extra attempts on timeout/5xx (default 1; 0 disables).
	SoftRetries *int `yaml:"soft_retries"`
	// FFProbePath is the ffprobe binary (default /usr/bin/ffprobe).
	FFProbePath string `yaml:"ffprobe_path"`
}

// ArtworkTLS is a per-host TLS exception for logo downloads only.
type ArtworkTLS struct {
	CAPem              string `yaml:"ca_pem"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"`
}

// Timeouts holds outbound and request-bound durations.
type Timeouts struct {
	// HTTPClient is the default timeout for outbound HTTP (providers, logos, etc.).
	HTTPClient time.Duration `yaml:"http_client"`
}

// Logging holds slog-related settings.
type Logging struct {
	Level string `yaml:"level"`
}

// New loads configuration: code defaults ← YAML overlay ← env overlay.
// Precedence: defaults → file → env (later layers win for set fields).
// An empty path skips the file (defaults + env only).
// A non-empty path that does not exist returns a wrapped os.ErrNotExist.
func New(path string) (*Config, error) {
	cfg := defaults()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("config: read %s: %w", path, err)
		}
		file := &Config{}
		if err := yaml.Unmarshal(data, file); err != nil {
			return nil, fmt.Errorf("config: parse %s: %w", path, err)
		}
		cfg.merge(file)
	}
	env, err := envOverlay()
	if err != nil {
		return nil, err
	}
	cfg.merge(env)

	cfg.BaseURL, err = NormalizeBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("config: base_url: %w", err)
	}
	cfg.ProxyBaseURL, err = NormalizeProxyBaseURL(cfg.ProxyBaseURL)
	if err != nil {
		return nil, fmt.Errorf("config: proxy_base_url: %w", err)
	}
	if cfg.ProxyAllEnabled() && cfg.ProxyBaseURL == "" {
		return nil, fmt.Errorf("config: proxy_all requires proxy_base_url")
	}
	if cfg.CacheLogosEnabled() && cfg.BaseURL == "" {
		return nil, fmt.Errorf("config: cache_logos requires base_url")
	}
	cfg.ArtworkTLS = normalizeArtworkTLSKeys(cfg.ArtworkTLS)
	if err := cfg.validateArtworkTLS(); err != nil {
		return nil, err
	}
	if cfg.Health.ConsecutiveFailures < 0 {
		return nil, fmt.Errorf("config: health.consecutive_failures must be >= 0")
	}
	if cfg.Health.L3HealthySample != nil {
		v := *cfg.Health.L3HealthySample
		if v < 0 || v > 1 {
			return nil, fmt.Errorf("config: health.l3_healthy_sample must be between 0 and 1")
		}
	}
	if cfg.Health.SoftRetries != nil && *cfg.Health.SoftRetries < 0 {
		return nil, fmt.Errorf("config: health.soft_retries must be >= 0")
	}

	providers, err := compileProviders(cfg.Providers)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	cfg.Providers = providers
	return cfg, nil
}

func defaults() *Config {
	proxyAll := false
	cacheLogos := false
	excludeUnhealthy := false
	l3Enabled := false
	return &Config{
		Listen:     DefaultListen,
		BaseURL:    "",
		DataDir:    DefaultDataDir,
		ProxyAll:   &proxyAll,
		CacheLogos: &cacheLogos,
		Timeouts: Timeouts{
			HTTPClient: 60 * time.Second,
		},
		Logging: Logging{
			Level: "info",
		},
		Health: Health{
			ConsecutiveFailures: 3,
			ExcludeUnhealthy:    &excludeUnhealthy,
			L2Interval:          24 * time.Hour,
			L2Workers:           4,
			L3Enabled:           &l3Enabled,
			L3Interval:          60 * time.Minute,
			L3Workers:           2,
			L3Timeout:           30 * time.Second,
			L3HealthySample:     floatPtr(0.1),
			MaxPerHost:          2,
			SoftRetries:         intPtr(1),
			FFProbePath:         "/usr/bin/ffprobe",
		},
	}
}

func floatPtr(v float64) *float64 { return &v }
func intPtr(v int) *int           { return &v }

// envOverlay builds a Config containing only values set in the environment.
func envOverlay() (*Config, error) {
	o := &Config{}
	if v := os.Getenv("PORT"); v != "" {
		o.Listen = NormalizeListen(v)
	}
	if v := os.Getenv("FASTGEN_BASE_URL"); v != "" {
		o.BaseURL = v
	}
	if v := os.Getenv("FASTGEN_DATA_DIR"); v != "" {
		o.DataDir = v
	}
	if v := os.Getenv("FASTGEN_PROXY_BASE_URL"); v != "" {
		o.ProxyBaseURL = v
	}
	if v := os.Getenv("FASTGEN_PROXY_ALL"); v != "" {
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("config: FASTGEN_PROXY_ALL: %w", err)
		}
		o.ProxyAll = &parsed
	}
	if v := os.Getenv("FASTGEN_CACHE_LOGOS"); v != "" {
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("config: FASTGEN_CACHE_LOGOS: %w", err)
		}
		o.CacheLogos = &parsed
	}
	if v := os.Getenv("FASTGEN_HEALTH_CONSECUTIVE_FAILURES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("config: FASTGEN_HEALTH_CONSECUTIVE_FAILURES: %w", err)
		}
		if n < 1 {
			return nil, fmt.Errorf("config: FASTGEN_HEALTH_CONSECUTIVE_FAILURES must be >= 1")
		}
		o.Health.ConsecutiveFailures = n
	}
	if v := os.Getenv("FASTGEN_HEALTH_EXCLUDE_UNHEALTHY"); v != "" {
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("config: FASTGEN_HEALTH_EXCLUDE_UNHEALTHY: %w", err)
		}
		o.Health.ExcludeUnhealthy = &parsed
	}
	if v := os.Getenv("FASTGEN_HEALTH_L2_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("config: FASTGEN_HEALTH_L2_INTERVAL: %w", err)
		}
		o.Health.L2Interval = d
	}
	if v := os.Getenv("FASTGEN_HEALTH_L3_ENABLED"); v != "" {
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("config: FASTGEN_HEALTH_L3_ENABLED: %w", err)
		}
		o.Health.L3Enabled = &parsed
	}
	if v := os.Getenv("FASTGEN_HEALTH_L3_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("config: FASTGEN_HEALTH_L3_INTERVAL: %w", err)
		}
		o.Health.L3Interval = d
	}
	if v := os.Getenv("FASTGEN_HEALTH_FFPROBE_PATH"); v != "" {
		o.Health.FFProbePath = v
	}
	if v := os.Getenv("FASTGEN_HEALTH_SOFT_RETRIES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("config: FASTGEN_HEALTH_SOFT_RETRIES: %w", err)
		}
		if n < 0 {
			return nil, fmt.Errorf("config: FASTGEN_HEALTH_SOFT_RETRIES must be >= 0")
		}
		o.Health.SoftRetries = &n
	}
	if v := os.Getenv("FASTGEN_HEALTH_L3_HEALTHY_SAMPLE"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, fmt.Errorf("config: FASTGEN_HEALTH_L3_HEALTHY_SAMPLE: %w", err)
		}
		if f < 0 || f > 1 {
			return nil, fmt.Errorf("config: FASTGEN_HEALTH_L3_HEALTHY_SAMPLE must be between 0 and 1")
		}
		o.Health.L3HealthySample = &f
	}
	if v := os.Getenv("FASTGEN_HEALTH_MAX_PER_HOST"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("config: FASTGEN_HEALTH_MAX_PER_HOST: %w", err)
		}
		if n < 1 {
			return nil, fmt.Errorf("config: FASTGEN_HEALTH_MAX_PER_HOST must be >= 1")
		}
		o.Health.MaxPerHost = n
	}
	if v := os.Getenv("FASTGEN_HEALTH_L2_WORKERS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("config: FASTGEN_HEALTH_L2_WORKERS: %w", err)
		}
		if n < 1 {
			return nil, fmt.Errorf("config: FASTGEN_HEALTH_L2_WORKERS must be >= 1")
		}
		o.Health.L2Workers = n
	}
	return o, nil
}

// LogLoaded writes structured startup lines for the deploy/server config.
// Per-provider effective settings are logged by provider.Registry.LogLoaded,
// since defaults are owned by the provider packages (not this YAML overlay).
func (c *Config) LogLoaded(path string, fromFile bool) {
	if c == nil {
		return
	}
	slog.Info("config loaded",
		"path", path,
		"from_file", fromFile,
		"listen", c.Listen,
		"base_url", c.BaseURL,
		"proxy_base_url", c.ProxyBaseURL,
		"proxy_all", c.ProxyAllEnabled(),
		"cache_logos", c.CacheLogosEnabled(),
		"data_dir", c.DataDir,
		"health_consecutive_failures", c.HealthConsecutiveFailures(),
		"health_exclude_unhealthy", c.HealthExcludeUnhealthy(),
		"health_l2_interval", c.HealthL2Interval().String(),
		"health_l3_enabled", c.HealthL3Enabled(),
		"health_ffprobe_path", c.HealthFFProbePath(),
		"provider_overlays", len(c.Providers),
	)
}

// merge overlays non-zero / non-empty fields from o onto c.
// Nil Providers means “not set”; a non-nil map (even empty) replaces Providers.
func (c *Config) merge(o *Config) {
	if c == nil || o == nil {
		return
	}
	if o.Listen != "" {
		c.Listen = o.Listen
	}
	if o.BaseURL != "" {
		c.BaseURL = o.BaseURL
	}
	if o.DataDir != "" {
		c.DataDir = o.DataDir
	}
	if o.ProxyBaseURL != "" {
		c.ProxyBaseURL = o.ProxyBaseURL
	}
	if o.ProxyAll != nil {
		value := *o.ProxyAll
		c.ProxyAll = &value
	}
	if o.CacheLogos != nil {
		value := *o.CacheLogos
		c.CacheLogos = &value
	}
	if o.ArtworkTLS != nil {
		c.ArtworkTLS = maps.Clone(o.ArtworkTLS)
	}
	if o.Timeouts.HTTPClient != 0 {
		c.Timeouts.HTTPClient = o.Timeouts.HTTPClient
	}
	if o.Logging.Level != "" {
		c.Logging.Level = o.Logging.Level
	}
	if o.Health.ConsecutiveFailures != 0 {
		c.Health.ConsecutiveFailures = o.Health.ConsecutiveFailures
	}
	if o.Health.ExcludeUnhealthy != nil {
		v := *o.Health.ExcludeUnhealthy
		c.Health.ExcludeUnhealthy = &v
	}
	if o.Health.L2Interval != 0 {
		c.Health.L2Interval = o.Health.L2Interval
	}
	if o.Health.L3Enabled != nil {
		v := *o.Health.L3Enabled
		c.Health.L3Enabled = &v
	}
	if o.Health.L3Interval != 0 {
		c.Health.L3Interval = o.Health.L3Interval
	}
	if o.Health.L3Workers != 0 {
		c.Health.L3Workers = o.Health.L3Workers
	}
	if o.Health.L2Workers != 0 {
		c.Health.L2Workers = o.Health.L2Workers
	}
	if o.Health.L3Timeout != 0 {
		c.Health.L3Timeout = o.Health.L3Timeout
	}
	if o.Health.L3HealthySample != nil {
		v := *o.Health.L3HealthySample
		c.Health.L3HealthySample = &v
	}
	if o.Health.MaxPerHost != 0 {
		c.Health.MaxPerHost = o.Health.MaxPerHost
	}
	if o.Health.SoftRetries != nil {
		v := *o.Health.SoftRetries
		c.Health.SoftRetries = &v
	}
	if o.Health.FFProbePath != "" {
		c.Health.FFProbePath = o.Health.FFProbePath
	}
	if o.Providers != nil {
		c.Providers = maps.Clone(o.Providers)
	}
}

// ProxyAllEnabled reports whether all exported streams should use FASTProxy.
func (c *Config) ProxyAllEnabled() bool {
	return c != nil && c.ProxyAll != nil && *c.ProxyAll
}

// CacheLogosEnabled reports whether channel logos should be downloaded and rewritten.
func (c *Config) CacheLogosEnabled() bool {
	return c != nil && c.CacheLogos != nil && *c.CacheLogos
}

// HealthConsecutiveFailures returns N for channel health DOWN (default 3, min 1).
func (c *Config) HealthConsecutiveFailures() int {
	if c == nil || c.Health.ConsecutiveFailures < 1 {
		return 3
	}
	return c.Health.ConsecutiveFailures
}

// HealthExcludeUnhealthy reports whether DOWN channels are pruned from export.
func (c *Config) HealthExcludeUnhealthy() bool {
	return c != nil && c.Health.ExcludeUnhealthy != nil && *c.Health.ExcludeUnhealthy
}

// HealthL2Interval returns the L2 segment probe interval (default 24h).
func (c *Config) HealthL2Interval() time.Duration {
	if c == nil || c.Health.L2Interval <= 0 {
		return 24 * time.Hour
	}
	return c.Health.L2Interval
}

// HealthL3Enabled reports whether scheduled L3 ffprobe is on.
func (c *Config) HealthL3Enabled() bool {
	return c != nil && c.Health.L3Enabled != nil && *c.Health.L3Enabled
}

// HealthL3Interval returns the L3 jitter window (default 60m).
func (c *Config) HealthL3Interval() time.Duration {
	if c == nil || c.Health.L3Interval <= 0 {
		return 60 * time.Minute
	}
	return c.Health.L3Interval
}

// HealthL3Workers returns L3 concurrency (default 2, min 1).
func (c *Config) HealthL3Workers() int {
	if c == nil || c.Health.L3Workers < 1 {
		return 2
	}
	return c.Health.L3Workers
}

// HealthL2Workers returns L2 concurrency (default 4, min 1).
func (c *Config) HealthL2Workers() int {
	if c == nil || c.Health.L2Workers < 1 {
		return 4
	}
	return c.Health.L2Workers
}

// HealthL3Timeout returns per-probe ffprobe timeout (default 30s).
func (c *Config) HealthL3Timeout() time.Duration {
	if c == nil || c.Health.L3Timeout <= 0 {
		return 30 * time.Second
	}
	return c.Health.L3Timeout
}

// HealthL3HealthySample returns fraction of healthy channels for L3 (default 0.1).
func (c *Config) HealthL3HealthySample() float64 {
	if c == nil || c.Health.L3HealthySample == nil {
		return 0.1
	}
	v := *c.Health.L3HealthySample
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// HealthMaxPerHost returns per-host probe concurrency (default 2, min 1).
func (c *Config) HealthMaxPerHost() int {
	if c == nil || c.Health.MaxPerHost < 1 {
		return 2
	}
	return c.Health.MaxPerHost
}

// HealthSoftRetries returns soft retries on timeout/5xx (default 1; 0 disables).
func (c *Config) HealthSoftRetries() int {
	if c == nil || c.Health.SoftRetries == nil {
		return 1
	}
	if *c.Health.SoftRetries < 0 {
		return 0
	}
	return *c.Health.SoftRetries
}

// HealthFFProbePath returns the ffprobe binary path.
func (c *Config) HealthFFProbePath() string {
	if c == nil || c.Health.FFProbePath == "" {
		return "/usr/bin/ffprobe"
	}
	return c.Health.FFProbePath
}

func normalizeArtworkTLSKeys(in map[string]ArtworkTLS) map[string]ArtworkTLS {
	if in == nil {
		return nil
	}
	out := make(map[string]ArtworkTLS, len(in))
	for host, policy := range in {
		key := strings.ToLower(strings.TrimSpace(host))
		out[key] = policy
	}
	return out
}

func (c *Config) validateArtworkTLS() error {
	if c == nil {
		return nil
	}
	for host, policy := range c.ArtworkTLS {
		if host == "" {
			return fmt.Errorf("config: artwork_tls: empty hostname")
		}
		if policy.CAPem == "" {
			continue
		}
		if _, err := parseCAPem(policy.CAPem); err != nil {
			return fmt.Errorf("config: artwork_tls[%s].ca_pem: %w", host, err)
		}
	}
	return nil
}

func parseCAPem(pemData string) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	rest := []byte(pemData)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		certs = append(certs, cert)
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("no certificates found")
	}
	return certs, nil
}

// ListenFromEnv returns the shared PORT listen address, or fallback if unset.
// Used by fastproxy (and other callers that need only a listen address).
func ListenFromEnv(fallback string) string {
	if v := os.Getenv("PORT"); v != "" {
		return NormalizeListen(v)
	}
	return NormalizeListen(fallback)
}

// NormalizeListen accepts "8180", ":8180", or "0.0.0.0:8180".
// A bare port (no ':') is rewritten as ":port" after strconv confirms it is an integer.
func NormalizeListen(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return v
	}
	if strings.Contains(v, ":") {
		return v
	}
	if _, err := strconv.Atoi(v); err != nil {
		return v
	}
	return ":" + v
}

// NormalizeBaseURL validates and canonicalizes the public fastgen origin.
func NormalizeBaseURL(value string) (string, error) {
	return normalizeAbsoluteOrigin(value)
}

// NormalizeProxyBaseURL validates and canonicalizes the public FASTProxy origin.
func NormalizeProxyBaseURL(value string) (string, error) {
	return normalizeAbsoluteOrigin(value)
}

func normalizeAbsoluteOrigin(value string) (string, error) {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" {
		return "", nil
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("must be an absolute http or https URL")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("must not contain a query or fragment")
	}
	return value, nil
}
