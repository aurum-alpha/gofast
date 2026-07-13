package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLoadMultiProviderYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := `
base_url: http://fastgen.lan:8180
providers:
  lg:
    label: LG
    chno_offset: 1000
    exclusions:
      - dinospluto-lgus
      - "(?i)blocked"
  pluto:
    enabled: true
    label: Pluto
    region: us
    slug_template: "plu-{id}.m3u8"
    refresh_interval: 3h
    min_channels: 50
  xumo:
    enabled: false
    label: Xumo
    synthesize_chno: 2000
    m3u_url: https://example.com/xumo.m3u
    epg_url: https://example.com/xumo.xml.gz
  localnow:
    label: LocalNow
    user_agent: "Mozilla/5.0 (compatible; GoFAST/0)"
    headers:
      Accept: text/plain
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Unsetenv("PORT")
	_ = os.Unsetenv("FASTGEN_BASE_URL")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "http://fastgen.lan:8180" {
		t.Fatalf("BaseURL: %q", cfg.BaseURL)
	}
	if len(cfg.Providers) != 4 {
		t.Fatalf("providers: got %d want 4 (YAML only, no baked-in set)", len(cfg.Providers))
	}

	lg := cfg.Providers["lg"]
	if !lg.IsEnabled() {
		t.Fatal("lg should default enabled")
	}
	if lg.Label != "LG" || lg.ChnoOffset != 1000 {
		t.Fatalf("lg: %+v", lg)
	}
	if len(lg.ExclusionRegexes) != 2 {
		t.Fatalf("lg exclusions compiled: %d", len(lg.ExclusionRegexes))
	}
	if !lg.ExclusionRegexes[0].MatchString("https://cdn/dinospluto-lgus/stream") {
		t.Fatal("expected dinospluto match")
	}
	if lg.RefreshInterval.Duration() != 6*time.Hour {
		t.Fatalf("lg default refresh: %v", lg.RefreshInterval)
	}

	pluto := cfg.Providers["pluto"]
	if pluto.SlugTemplate != "plu-{id}.m3u8" || pluto.Region != "us" {
		t.Fatalf("pluto: %+v", pluto)
	}
	if pluto.RefreshInterval.Duration() != 3*time.Hour || pluto.MinChannels != 50 {
		t.Fatalf("pluto refresh/min: %+v", pluto)
	}

	xumo := cfg.Providers["xumo"]
	if xumo.IsEnabled() {
		t.Fatal("xumo should be disabled")
	}
	if xumo.SynthesizeChno != 2000 || xumo.M3UURL == "" {
		t.Fatalf("xumo: %+v", xumo)
	}

	ln := cfg.Providers["localnow"]
	if !strings.Contains(ln.UserAgent, "GoFAST") {
		t.Fatalf("localnow UA: %q", ln.UserAgent)
	}
	if ln.Headers["Accept"] != "text/plain" {
		t.Fatalf("localnow headers: %+v", ln.Headers)
	}
	if _, ok := cfg.Providers["samsung"]; ok {
		t.Fatal("samsung must not appear unless listed in YAML")
	}
}

func TestLoadBadExclusionRegex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := `
providers:
  lg:
    exclusions:
      - "(unclosed"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected regex compile error")
	}
	if !strings.Contains(err.Error(), "exclusions") {
		t.Fatalf("error should mention exclusions: %v", err)
	}
}

func TestDefaultsHaveNoProviders(t *testing.T) {
	cfg := Defaults()
	if len(cfg.Providers) != 0 {
		t.Fatalf("code defaults must not bake in providers, got %d", len(cfg.Providers))
	}
}

func TestLoadWithoutProvidersFile(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Providers) != 0 {
		t.Fatalf("empty path must not invent providers, got %d", len(cfg.Providers))
	}
}

func TestExampleConfigLoads(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "config.example.yaml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"lg", "pluto", "samsung", "roku", "xumo", "distrotv", "localnow"}
	if len(cfg.Providers) != len(want) {
		t.Fatalf("example providers: got %d want %d", len(cfg.Providers), len(want))
	}
	for _, id := range want {
		if _, ok := cfg.Providers[id]; !ok {
			t.Fatalf("example missing %q", id)
		}
	}
	if !cfg.Providers["lg"].ExclusionRegexes[0].MatchString("dinospluto-lgus") {
		t.Fatal("example lg exclusion did not compile")
	}
}

func TestProviderIsEnabled(t *testing.T) {
	if !(Provider{}).IsEnabled() {
		t.Fatal("nil enabled => true")
	}
	f := false
	if (Provider{Enabled: &f}).IsEnabled() {
		t.Fatal("false => disabled")
	}
}
