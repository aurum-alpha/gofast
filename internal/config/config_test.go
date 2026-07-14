package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
)

func clearDeployEnv(t *testing.T) {
	t.Helper()
	t.Setenv("PORT", "")
	t.Setenv("FASTGEN_BASE_URL", "")
	t.Setenv("FASTGEN_DATA_DIR", "")
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaults(t *testing.T) {
	cfg := defaults()
	if cfg.Listen != DefaultListen {
		t.Fatalf("Listen: got %q want %q", cfg.Listen, DefaultListen)
	}
	if cfg.DataDir != DefaultDataDir {
		t.Fatalf("DataDir: got %q want %q", cfg.DataDir, DefaultDataDir)
	}
	if cfg.Timeouts.HTTPClient != 60*time.Second {
		t.Fatalf("HTTPClient: got %v", cfg.Timeouts.HTTPClient)
	}
	if cfg.Logging.Level != "info" {
		t.Fatalf("Level: got %q", cfg.Logging.Level)
	}
	if len(cfg.Providers) != 0 {
		t.Fatalf("defaults must not bake in providers, got %d", len(cfg.Providers))
	}
}

func TestNewFromYAML(t *testing.T) {
	clearDeployEnv(t)
	path := writeConfig(t, `
listen: ":9090"
base_url: "http://example.local:9090"
data_dir: "/var/gofast"
timeouts:
  http_client: 30s
logging:
  level: debug
`)
	cfg, err := New(path)
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
	if cfg.Timeouts.HTTPClient != 30*time.Second {
		t.Errorf("HTTPClient: got %v", cfg.Timeouts.HTTPClient)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("Level: got %q", cfg.Logging.Level)
	}
}

func TestNewEnvOverridesYAML(t *testing.T) {
	path := writeConfig(t, `
listen: ":9090"
base_url: "http://from-file"
data_dir: "/from-file"
`)
	t.Setenv("PORT", "8180")
	t.Setenv("FASTGEN_BASE_URL", "http://from-env")
	t.Setenv("FASTGEN_DATA_DIR", "/from-env")

	cfg, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":8180" {
		t.Errorf("Listen: PORT should win, got %q", cfg.Listen)
	}
	if cfg.BaseURL != "http://from-env" {
		t.Errorf("BaseURL: env should win, got %q", cfg.BaseURL)
	}
	if cfg.DataDir != "/from-env" {
		t.Errorf("DataDir: env should win, got %q", cfg.DataDir)
	}
}

func TestNewEmptyPathDefaultsAndEnv(t *testing.T) {
	t.Setenv("PORT", ":7777")
	t.Setenv("FASTGEN_BASE_URL", "http://env-only")
	t.Setenv("FASTGEN_DATA_DIR", "/tmp/data")

	cfg, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":7777" || cfg.BaseURL != "http://env-only" || cfg.DataDir != "/tmp/data" {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
	if cfg.Timeouts.HTTPClient != 60*time.Second {
		t.Fatalf("expected default timeout, got %v", cfg.Timeouts.HTTPClient)
	}
	if len(cfg.Providers) != 0 {
		t.Fatalf("empty path must not invent providers, got %d", len(cfg.Providers))
	}
}

func TestNewMissingFile(t *testing.T) {
	_, err := New(filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want os.ErrNotExist, got %v", err)
	}
}

func TestNewPartialYAMLKeepsDefaults(t *testing.T) {
	clearDeployEnv(t)
	path := writeConfig(t, "base_url: http://only-base\n")
	cfg, err := New(path)
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
	if cfg.Timeouts.HTTPClient != 60*time.Second {
		t.Errorf("HTTPClient default lost: %v", cfg.Timeouts.HTTPClient)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("Level default lost: %q", cfg.Logging.Level)
	}
}

func TestNewDurationYAML(t *testing.T) {
	clearDeployEnv(t)
	path := writeConfig(t, "timeouts:\n  http_client: 45s\n")
	cfg, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Timeouts.HTTPClient != 45*time.Second {
		t.Fatalf("got %v want 45s", cfg.Timeouts.HTTPClient)
	}
}

func TestMergeNilOverlay(t *testing.T) {
	cfg := defaults()
	cfg.merge(nil)
	if cfg.Listen != DefaultListen {
		t.Fatalf("nil overlay changed Listen: %q", cfg.Listen)
	}
}

func TestMergeOverlaysSetFieldsOnly(t *testing.T) {
	cfg := defaults()
	cfg.merge(&Config{BaseURL: "http://from-overlay"})
	if cfg.BaseURL != "http://from-overlay" {
		t.Fatalf("BaseURL: %q", cfg.BaseURL)
	}
	if cfg.Listen != DefaultListen || cfg.DataDir != DefaultDataDir {
		t.Fatalf("unset fields changed: listen=%q data_dir=%q", cfg.Listen, cfg.DataDir)
	}
	if cfg.Timeouts.HTTPClient != 60*time.Second || cfg.Logging.Level != "info" {
		t.Fatalf("defaults lost: %+v", cfg)
	}
}

func TestMergeProviders(t *testing.T) {
	cfg := defaults()
	cfg.Providers = map[string]model.ProviderSettings{"keep": {Label: "Keep"}}

	cfg.merge(&Config{}) // Providers nil → keep base
	if _, ok := cfg.Providers["keep"]; !ok {
		t.Fatal("nil Providers overlay must leave base providers")
	}

	cfg.merge(&Config{
		Providers: map[string]model.ProviderSettings{"lg": {Label: "LG"}},
	})
	if len(cfg.Providers) != 1 || cfg.Providers["lg"].Label != "LG" {
		t.Fatalf("non-nil Providers must replace: %+v", cfg.Providers)
	}
	if _, ok := cfg.Providers["keep"]; ok {
		t.Fatal("replaced map must not keep old keys")
	}

	cfg.merge(&Config{Providers: map[string]model.ProviderSettings{}})
	if cfg.Providers == nil || len(cfg.Providers) != 0 {
		t.Fatalf("empty Providers map must replace with empty: %+v", cfg.Providers)
	}
}

func TestNormalizeListen(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"8180", ":8180"},
		{":8180", ":8180"},
		{"0.0.0.0:8180", "0.0.0.0:8180"},
		{" 9191 ", ":9191"},
		{"", ""},
		{"not-a-port", "not-a-port"},
	}
	for _, tt := range tests {
		if got := NormalizeListen(tt.in); got != tt.want {
			t.Errorf("NormalizeListen(%q)=%q want %q", tt.in, got, tt.want)
		}
	}
}

func TestListenFromEnv(t *testing.T) {
	t.Run("port set", func(t *testing.T) {
		t.Setenv("PORT", "9191")
		if got := ListenFromEnv(":8181"); got != ":9191" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("fallback", func(t *testing.T) {
		t.Setenv("PORT", "")
		if got := ListenFromEnv("8181"); got != ":8181" {
			t.Fatalf("fallback got %q", got)
		}
	})
}

func TestEnvOverlay(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("FASTGEN_BASE_URL", "http://env")
	t.Setenv("FASTGEN_DATA_DIR", "/env-data")
	o := envOverlay()
	if o.Listen != ":8080" || o.BaseURL != "http://env" || o.DataDir != "/env-data" {
		t.Fatalf("%+v", o)
	}
	if o.Timeouts.HTTPClient != 0 || o.Logging.Level != "" || o.Providers != nil {
		t.Fatalf("overlay must only set env fields: %+v", o)
	}
}
