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

// Health holds channel-health FSM knobs (attr store is separate).
type Health struct {
	// ConsecutiveFailures is N for DOWN (default 3). Zero in YAML means unset.
	ConsecutiveFailures int `yaml:"consecutive_failures"`
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
		},
	}
}

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
