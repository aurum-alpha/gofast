// Package config loads deploy and runtime settings for fastgen / fastproxy.
//
// Deploy-varying values follow https://12factor.net/config: environment variables
// are the primary override. An optional YAML file on the data volume may supply
// structured defaults; it is never required when env + code defaults suffice.
package config

import (
	"fmt"
	"maps"
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
	Listen    string                    `yaml:"listen"`
	BaseURL   string                    `yaml:"base_url"`
	DataDir   string                    `yaml:"data_dir"`
	Timeouts  Timeouts                  `yaml:"timeouts"`
	Logging   Logging                   `yaml:"logging"`
	Providers map[string]model.Provider `yaml:"providers"`
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
	cfg.merge(envOverlay())

	providers, err := compileProviders(cfg.Providers)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	cfg.Providers = providers
	return cfg, nil
}

func defaults() *Config {
	return &Config{
		Listen:  DefaultListen,
		BaseURL: "",
		DataDir: DefaultDataDir,
		Timeouts: Timeouts{
			HTTPClient: 60 * time.Second,
		},
		Logging: Logging{
			Level: "info",
		},
	}
}

// envOverlay builds a Config containing only values set in the environment.
func envOverlay() *Config {
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
	return o
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
	if o.Timeouts.HTTPClient != 0 {
		c.Timeouts.HTTPClient = o.Timeouts.HTTPClient
	}
	if o.Logging.Level != "" {
		c.Logging.Level = o.Logging.Level
	}
	if o.Providers != nil {
		c.Providers = maps.Clone(o.Providers)
	}
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
