package refresh

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/logocache"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/providerset"
)

// TestServiceReloadEnableDisableProvider runs the full hot-reload loop against
// a real adapter (LG parsing its schedulelist fixture from a local server):
// boot enabled → fetch → disable via Store.Save (feed drops, cache kept) →
// re-enable (instant warm restore, no network) → live settings edit.
func TestServiceReloadEnableDisableProvider(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "provider", "lg", "testdata", "schedulelist.json"))
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer upstream.Close()

	dir := t.TempDir()
	body := "providers:\n  lg:\n    enabled: true\n    min_channels: 1\n    refresh_interval: 1h\n    channels_url: " + upstream.URL + "\n"
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	store := config.NewStore(path)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}

	cfg := store.Current()
	settings := providerset.Settings(cfg.Providers, cfg.EffectiveRegions())
	readers := providerset.Readers(settings, nil)
	reg := provider.NewRegistry(readers, settings)
	cc := cache.New(filepath.Join(dir, "cache"))
	var notified atomic.Int32
	svc := New(store, reg, nil, cc, nil, nil, nil, func() { notified.Add(1) }, nil)
	store.Register("refresh", svc)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	svc.Run(ctx)

	// Boot: the scheduled loop fetches immediately (no prior FetchedAt).
	feed, ok := reg.Feed(model.ProviderLG)
	if !ok {
		t.Fatal("lg feed missing at boot")
	}
	waitFor(t, 5*time.Second, "initial fetch", func() bool { return !feed.FetchedAt().IsZero() })
	if len(feed.Channels()) == 0 {
		t.Fatal("no channels after fetch")
	}

	// Disable live: feed drops from the registry, cache stays on disk.
	if _, err := store.Save(ctx, "", []config.PathOp{{Path: "providers.lg.enabled", Value: false}}); err != nil {
		t.Fatalf("disable save: %v", err)
	}
	if _, ok := reg.Feed(model.ProviderLG); ok {
		t.Fatal("feed still present after disable")
	}
	if reg.Settings(model.ProviderLG).IsEnabled() {
		t.Fatal("settings should record disabled")
	}
	if _, _, _, err := cc.LoadProvider(model.ProviderLG); err != nil {
		t.Fatalf("cache should survive disable: %v", err)
	}

	// Re-enable live: warm restore from cache, synchronously, without network.
	fetchesBefore := hits.Load()
	if _, err := store.Save(ctx, "", []config.PathOp{{Path: "providers.lg.enabled", Value: true}}); err != nil {
		t.Fatalf("enable save: %v", err)
	}
	feed, ok = reg.Feed(model.ProviderLG)
	if !ok {
		t.Fatal("feed missing after enable")
	}
	if len(feed.Channels()) == 0 {
		t.Fatal("warm enable should restore channels instantly")
	}
	if hits.Load() != fetchesBefore {
		t.Fatalf("warm enable hit the network: %d -> %d", fetchesBefore, hits.Load())
	}

	// Live settings edit on a running provider.
	if _, err := store.Save(ctx, "", []config.PathOp{{Path: "providers.lg.label", Value: "LG TV"}}); err != nil {
		t.Fatalf("label save: %v", err)
	}
	if got := feed.Label(); got != "LG TV" {
		t.Fatalf("label = %q", got)
	}
	if notified.Load() == 0 {
		t.Fatal("aggregate was never notified")
	}
}

func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestServiceReloadRewritesLogosOnBaseURLChange ensures a base_url change while
// cache_logos is on re-emits /logos/ URLs with the new origin (no proactive fetch).
func TestServiceReloadRewritesLogosOnBaseURLChange(t *testing.T) {
	logoSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png"))
	}))
	t.Cleanup(logoSrv.Close)

	enabled := true
	cacheLogos := true
	// Overlay only — Reload compares against providerset.Settings merges, so the
	// live feed must carry that same merged settings to avoid Upsert'ing the real LG reader.
	overlay := map[model.ProviderID]model.ProviderSettings{
		model.ProviderLG: {Enabled: &enabled, MinChannels: 1, Label: "LG"},
	}
	settings := providerset.Settings(overlay, "")[model.ProviderLG]
	reg := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{model.ProviderLG: logoReader{logoURL: logoSrv.URL + "/a.png"}},
		map[model.ProviderID]model.ProviderSettings{model.ProviderLG: settings},
	)
	feed, _ := reg.Feed(model.ProviderLG)
	cc := cache.New(t.TempDir())
	oldBase := "http://old.example:8180"
	newBase := "http://new.example:8180"
	logos := logocache.New(cc, logoSrv.Client(), oldBase, time.Hour)
	t.Cleanup(logos.Close)
	pr := &providerRefresher{feed: feed, cache: cc, pipe: &pipeline{logos: logos}}
	if err := pr.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	m3u, err := cc.ReadM3U(model.ProviderLG)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(m3u), oldBase+"/logos/lg/") {
		t.Fatalf("want old base in m3u before reload: %s", m3u)
	}

	svc := New(nil, reg, nil, cc, nil, nil, nil, nil, nil)
	svc.pipe.set(EmissionPolicy{}, nil, nil, logos)
	svc.logosSrc = logos
	pr.pipe = svc.pipe
	svc.running[model.ProviderLG] = &runningProvider{pr: pr, kick: make(chan struct{}, 1)}
	svc.applied = &config.Config{
		BaseURL:    oldBase,
		CacheLogos: &cacheLogos,
		Providers:  overlay,
	}

	newCfg := &config.Config{
		BaseURL:    newBase,
		CacheLogos: &cacheLogos,
		Providers:  overlay,
	}
	if err := svc.Reload(context.Background(), newCfg); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, "logos rewritten to new base_url", func() bool {
		data, err := cc.ReadM3U(model.ProviderLG)
		chs := feed.Channels()
		return err == nil &&
			strings.Contains(string(data), newBase+"/logos/lg/") &&
			len(chs) > 0 &&
			strings.HasPrefix(chs[0].LogoURL, newBase+"/logos/")
	})
}
