package config

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
)

func TestNewMultiProviderYAML(t *testing.T) {
	clearDeployEnv(t)
	path := writeConfig(t, `
base_url: http://fastgen.lan:8180
providers:
  lg:
    label: LG
    channel_number_offset: 1000
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
    synthesize_channel_numbers: 2000
    m3u_url: https://example.com/xumo.m3u
    epg_url: https://example.com/xumo.xml.gz
  localnow:
    label: LocalNow
    user_agent: "Mozilla/5.0 (compatible; GoFAST/0)"
    headers:
      Accept: text/plain
`)
	cfg, err := New(path)
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
	if lg.Label != "LG" || lg.ChannelNumberOffset != 1000 {
		t.Fatalf("lg: %+v", lg)
	}
	if len(lg.ExclusionRegexes) != 2 {
		t.Fatalf("lg exclusions compiled: %d", len(lg.ExclusionRegexes))
	}
	if !lg.ExclusionRegexes[0].MatchString("https://cdn/dinospluto-lgus/stream") {
		t.Fatal("expected dinospluto match")
	}
	// cfg.Providers is the raw YAML overlay only; per-field defaults (e.g. the
	// 6h refresh) are owned by the provider packages and merged in the bootstrap.
	if lg.RefreshInterval != 0 {
		t.Fatalf("overlay should not carry a default refresh: %v", lg.RefreshInterval)
	}

	pluto := cfg.Providers["pluto"]
	if pluto.SlugTemplate != "plu-{id}.m3u8" || pluto.Region != "us" {
		t.Fatalf("pluto: %+v", pluto)
	}
	if pluto.RefreshInterval != 3*time.Hour || pluto.MinChannels != 50 {
		t.Fatalf("pluto refresh/min: %+v", pluto)
	}

	xumo := cfg.Providers["xumo"]
	if xumo.IsEnabled() {
		t.Fatal("xumo should be disabled")
	}
	if xumo.SynthesizeChannelNumbers != 2000 || xumo.M3UURL == "" {
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

func TestNewExplicitZeroChannelNumberOffset(t *testing.T) {
	clearDeployEnv(t)
	path := writeConfig(t, `
providers:
  lg:
    channel_number_offset: 0
`)
	cfg, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Providers["lg"].ChannelNumberOffset; got != 0 {
		t.Fatalf("overlay offset: got %d want 0", got)
	}
}

func TestNewBadExclusionRegex(t *testing.T) {
	clearDeployEnv(t)
	path := writeConfig(t, `
providers:
  lg:
    exclusions:
      - "(unclosed"
`)
	_, err := New(path)
	if err == nil {
		t.Fatal("expected regex compile error")
	}
	if !strings.Contains(err.Error(), "exclusions") {
		t.Fatalf("error should mention exclusions: %v", err)
	}
}

func TestNewExampleConfig(t *testing.T) {
	clearDeployEnv(t)
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "config.example.yaml")
	cfg, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []model.ProviderID{"lg", "pluto", "samsung", "roku", "plex", "xumo", "tubi", "tcl", "distrotv", "localnow"}
	if len(cfg.Providers) != len(want) {
		t.Fatalf("example providers: got %d want %d", len(cfg.Providers), len(want))
	}
	for _, id := range want {
		if _, ok := cfg.Providers[id]; !ok {
			t.Fatalf("example missing %q", id)
		}
	}
	for _, id := range []model.ProviderID{
		model.ProviderDistroTV,
		model.ProviderLocalNow,
		model.ProviderPlex,
		model.ProviderPluto,
		model.ProviderRoku,
		model.ProviderSamsung,
		model.ProviderTCL,
		model.ProviderTubi,
		model.ProviderXumo,
	} {
		if !cfg.Providers[id].IsEnabled() {
			t.Fatalf("example %q should be enabled", id)
		}
	}
	if !cfg.Providers["lg"].ExclusionRegexes[0].MatchString("dinospluto-lgus") {
		t.Fatal("example lg exclusion did not compile")
	}

	list := model.ListProviders(cfg.Providers)
	if len(list.Providers) != 10 || list.Providers[0].ID != "distrotv" {
		t.Fatalf("sorted list: %+v", list.Providers)
	}
}
