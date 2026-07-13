// Package config loads deploy and runtime settings for fastgen.
//
// Deploy-varying values follow https://12factor.net/config: environment variables
// are the primary override. An optional YAML file on the data volume may supply
// structured defaults; it is never required when env + code defaults suffice.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultPath    = "/data/config.yaml"
	DefaultListen  = ":8180"
	DefaultDataDir = "/data"
)

// Config is the core (M0) configuration surface.
// Provider, proxy, and health layers land in later issues.
type Config struct {
	Listen   string   `yaml:"listen"`
	BaseURL  string   `yaml:"base_url"`
	DataDir  string   `yaml:"data_dir"`
	Timeouts Timeouts `yaml:"timeouts"`
	Logging  Logging  `yaml:"logging"`
}

// Timeouts holds outbound and request-bound durations.
type Timeouts struct {
	// HTTPClient is the default timeout for outbound HTTP (providers, logos, etc.).
	HTTPClient Duration `yaml:"http_client"`
}

// Logging holds slog-related settings.
type Logging struct {
	Level string `yaml:"level"`
}

// Defaults returns code defaults (not deploy-specific).
func Defaults() Config {
	return Config{
		Listen:  DefaultListen,
		BaseURL: "",
		DataDir: DefaultDataDir,
		Timeouts: Timeouts{
			HTTPClient: Duration(60 * time.Second),
		},
		Logging: Logging{
			Level: "info",
		},
	}
}

// Load reads YAML from path (if non-empty), then applies environment overrides.
// Precedence: defaults → file → env.
// An empty path skips the file (env + defaults only).
// A non-empty path that does not exist returns a wrapped os.ErrNotExist.
func Load(path string) (Config, error) {
	cfg := Defaults()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("config: read %s: %w", path, err)
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
		}
		// Unmarshal replaces the whole struct; re-apply zero fields with defaults.
		cfg = mergeDefaults(cfg)
	}
	ApplyEnv(&cfg)
	return cfg, nil
}

// ApplyEnv overlays FASTGEN_* deploy vars. Env always wins for overlapping keys.
func ApplyEnv(cfg *Config) {
	if v := os.Getenv("FASTGEN_LISTEN"); v != "" {
		cfg.Listen = v
	}
	if v := os.Getenv("FASTGEN_BASE_URL"); v != "" {
		cfg.BaseURL = v
	}
	if v := os.Getenv("FASTGEN_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
}

func mergeDefaults(cfg Config) Config {
	d := Defaults()
	if cfg.Listen == "" {
		cfg.Listen = d.Listen
	}
	if cfg.DataDir == "" {
		cfg.DataDir = d.DataDir
	}
	if cfg.Timeouts.HTTPClient == 0 {
		cfg.Timeouts.HTTPClient = d.Timeouts.HTTPClient
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = d.Logging.Level
	}
	return cfg
}
