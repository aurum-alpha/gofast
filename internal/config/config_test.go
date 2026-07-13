package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	if cfg.Listen != DefaultListen {
		t.Fatalf("Listen: got %q want %q", cfg.Listen, DefaultListen)
	}
	if cfg.DataDir != DefaultDataDir {
		t.Fatalf("DataDir: got %q want %q", cfg.DataDir, DefaultDataDir)
	}
	if cfg.Timeouts.HTTPClient.Duration() != 60*time.Second {
		t.Fatalf("HTTPClient: got %v", cfg.Timeouts.HTTPClient)
	}
	if cfg.Logging.Level != "info" {
		t.Fatalf("Level: got %q", cfg.Logging.Level)
	}
}

func TestLoadParse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := `
listen: ":9090"
base_url: "http://example.local:9090"
data_dir: "/var/gofast"
timeouts:
  http_client: 30s
logging:
  level: debug
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("FASTGEN_LISTEN", "")
	t.Setenv("FASTGEN_BASE_URL", "")
	t.Setenv("FASTGEN_DATA_DIR", "")
	_ = os.Unsetenv("FASTGEN_LISTEN")
	_ = os.Unsetenv("FASTGEN_BASE_URL")
	_ = os.Unsetenv("FASTGEN_DATA_DIR")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":9090" {
		t.Errorf("Listen: got %q", cfg.Listen)
	}
	if cfg.BaseURL != "http://example.local:9090" {
		t.Errorf("BaseURL: got %q", cfg.BaseURL)
	}
	if cfg.DataDir != "/var/gofast" {
		t.Errorf("DataDir: got %q", cfg.DataDir)
	}
	if cfg.Timeouts.HTTPClient.Duration() != 30*time.Second {
		t.Errorf("HTTPClient: got %v", cfg.Timeouts.HTTPClient)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("Level: got %q", cfg.Logging.Level)
	}
}

func TestLoadEnvPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := `
listen: ":9090"
base_url: "http://from-file"
data_dir: "/from-file"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("FASTGEN_LISTEN", ":8180")
	t.Setenv("FASTGEN_BASE_URL", "http://from-env")
	t.Setenv("FASTGEN_DATA_DIR", "/from-env")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":8180" {
		t.Errorf("Listen: env should win, got %q", cfg.Listen)
	}
	if cfg.BaseURL != "http://from-env" {
		t.Errorf("BaseURL: env should win, got %q", cfg.BaseURL)
	}
	if cfg.DataDir != "/from-env" {
		t.Errorf("DataDir: env should win, got %q", cfg.DataDir)
	}
}

func TestLoadEmptyPathUsesDefaultsAndEnv(t *testing.T) {
	t.Setenv("FASTGEN_LISTEN", ":7777")
	t.Setenv("FASTGEN_BASE_URL", "http://env-only")
	t.Setenv("FASTGEN_DATA_DIR", "/tmp/data")

	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":7777" || cfg.BaseURL != "http://env-only" || cfg.DataDir != "/tmp/data" {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
	if cfg.Timeouts.HTTPClient.Duration() != 60*time.Second {
		t.Fatalf("expected default timeout, got %v", cfg.Timeouts.HTTPClient)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want os.ErrNotExist, got %v", err)
	}
}

func TestLoadPartialYAMLKeepsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("base_url: http://only-base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Unsetenv("FASTGEN_LISTEN")
	_ = os.Unsetenv("FASTGEN_BASE_URL")
	_ = os.Unsetenv("FASTGEN_DATA_DIR")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != DefaultListen {
		t.Errorf("Listen default lost: %q", cfg.Listen)
	}
	if cfg.DataDir != DefaultDataDir {
		t.Errorf("DataDir default lost: %q", cfg.DataDir)
	}
	if cfg.BaseURL != "http://only-base" {
		t.Errorf("BaseURL: got %q", cfg.BaseURL)
	}
}

func TestDurationYAML(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want time.Duration
	}{
		{"string", "timeouts:\n  http_client: 45s\n", 45 * time.Second},
		{"seconds_int", "timeouts:\n  http_client: 15\n", 15 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "c.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0o644); err != nil {
				t.Fatal(err)
			}
			_ = os.Unsetenv("FASTGEN_LISTEN")
			cfg, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Timeouts.HTTPClient.Duration() != tt.want {
				t.Fatalf("got %v want %v", cfg.Timeouts.HTTPClient, tt.want)
			}
		})
	}
}
