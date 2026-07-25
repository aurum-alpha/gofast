package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T, body string) *Store {
	t.Helper()
	clearDeployEnv(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store := NewStore(path)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestStoreLoadMissingFileWritesDefaults(t *testing.T) {
	store := newTestStore(t, "")
	if _, err := os.Stat(store.Path()); err != nil {
		t.Fatalf("defaults not written: %v", err)
	}
	if store.Current() == nil || store.Revision() == "" {
		t.Fatal("snapshot or revision missing after load")
	}
	if !store.FromFile() {
		t.Fatal("expected from_file=true after defaults were written")
	}
}

func TestStoreSaveAppliesAndReloads(t *testing.T) {
	store := newTestStore(t, "listen: \":8180\"\n# operator note\n")
	before := store.Revision()

	results, err := store.Save(context.Background(), before, []PathOp{
		{Path: "cache_logos", Value: false},
		{Path: "health.l1_interval", Value: "12h"},
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if results == nil {
		t.Fatal("nil reload results")
	}
	if store.Revision() == before {
		t.Fatal("revision did not change")
	}
	cfg := store.Current()
	if cfg.HealthL1Interval() != 12*time.Hour {
		t.Fatalf("l1_interval = %s", cfg.HealthL1Interval())
	}
	data, _ := os.ReadFile(store.Path())
	if !strings.Contains(string(data), "# operator note") {
		t.Fatalf("comment lost:\n%s", data)
	}
	if _, err := os.Stat(store.Path() + ".bak"); err != nil {
		t.Fatalf(".bak missing: %v", err)
	}
}

func TestStoreSaveStaleRevision(t *testing.T) {
	store := newTestStore(t, "listen: \":8180\"\n")
	if _, err := store.Save(context.Background(), "deadbeef", []PathOp{{Path: "cache_logos", Value: false}}); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("expected ErrStaleRevision, got %v", err)
	}
}

func TestStoreSaveInvalidCandidateLeavesFileUntouched(t *testing.T) {
	body := "listen: \":8180\"\n"
	store := newTestStore(t, body)
	rev := store.Revision()

	// proxy_all without proxy_base_url fails New's validation.
	_, err := store.Save(context.Background(), rev, []PathOp{{Path: "proxy_all", Value: true}})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid, got %v", err)
	}
	data, _ := os.ReadFile(store.Path())
	if string(data) != body {
		t.Fatalf("file changed on invalid save:\n%s", data)
	}
	if store.Revision() != rev {
		t.Fatal("revision changed on invalid save")
	}
}

func TestStoreSaveKicksReloadersInOrder(t *testing.T) {
	store := newTestStore(t, "listen: \":8180\"\n")
	var order []string
	store.Register("first", ReloaderFunc(func(ctx context.Context, cfg *Config) error {
		if !cfg.CacheLogosEnabled() {
			t.Error("reloader saw stale snapshot")
		}
		order = append(order, "first")
		return nil
	}))
	store.Register("second", ReloaderFunc(func(ctx context.Context, cfg *Config) error {
		order = append(order, "second")
		return fmt.Errorf("boom")
	}))

	results, err := store.Save(context.Background(), "", []PathOp{
		{Path: "base_url", Value: "http://example:8180"},
		{Path: "cache_logos", Value: true},
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("order = %v", order)
	}
	if len(results) != 2 || results[0].Error != "" || results[1].Error != "boom" {
		t.Fatalf("results = %+v", results)
	}
}

func TestStoreSaveNoPath(t *testing.T) {
	clearDeployEnv(t)
	store := NewStore("")
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(context.Background(), "", nil); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("expected ErrReadOnly, got %v", err)
	}
}

func TestEnvShadow(t *testing.T) {
	clearDeployEnv(t)
	t.Setenv("FASTGEN_BASE_URL", "http://env:8180")
	t.Setenv("FASTGEN_HEALTH_L2_INTERVAL", "6h") // legacy alias for l1_interval
	shadow := EnvShadow()
	if shadow["base_url"] != "FASTGEN_BASE_URL" {
		t.Fatalf("base_url shadow = %q", shadow["base_url"])
	}
	if shadow["health.l1_interval"] != "FASTGEN_HEALTH_L2_INTERVAL" {
		t.Fatalf("l1_interval shadow = %q", shadow["health.l1_interval"])
	}
	if _, ok := shadow["cache_logos"]; ok {
		t.Fatal("cache_logos should not be shadowed")
	}
}

func TestStoreChannelEmitRoundTripAndOrphanRetention(t *testing.T) {
	store := newTestStore(t, `providers:
  lg:
    enabled: true
    min_channels: 1
    channel_emit:
      gone.channel:
        name: Kept
      live:
        export: disabled
`)
	cfg := store.Current()
	emits := cfg.Providers["lg"].ChannelEmit
	if emits["gone.channel"].Name != "Kept" || emits["live"].Export != "disabled" {
		t.Fatalf("loaded emit: %+v", emits)
	}
	// Unrelated save must retain the absent-channel key.
	if _, err := store.Save(context.Background(), store.Revision(), []PathOp{
		{Path: "logging.level", Value: "debug"},
	}); err != nil {
		t.Fatal(err)
	}
	emits = store.Current().Providers["lg"].ChannelEmit
	if emits["gone.channel"].Name != "Kept" {
		t.Fatalf("orphan emit lost: %+v", emits)
	}
	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "gone.channel") || !strings.Contains(string(raw), "Kept") {
		t.Fatalf("yaml lost orphan emit:\n%s", raw)
	}
}
