package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/channelattr"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/refresh"
	"github.com/j27-aurum/gofast/internal/server"
)

func TestCacheInventoryAPI(t *testing.T) {
	dataDir := t.TempDir()
	cc := cache.New(filepath.Join(dataDir, "cache"))
	if err := cc.CommitProvider("lg", provider.Raw{"schedule.json": []byte("RAW")}, cache.M3U("#"), cache.XMLTV("<tv/>"), provider.Meta{}); err != nil {
		t.Fatal(err)
	}
	attrs, err := channelattr.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = attrs.Close() })
	v, _ := json.Marshal(model.ChannelHealth{Status: model.HealthHealthy})
	if err := attrs.Handle(context.Background(), channelattr.Event{
		Provider:  model.ProviderLG,
		ChannelID: "news",
		Kind:      channelattr.KindHealth,
		Value:     v,
		At:        time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	h := server.CacheInventoryHandler(cc, attrs)
	req := httptest.NewRequest(http.MethodGet, "/api/cache", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var body server.CacheInventoryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.GenerationCount < 1 || body.ChannelAttr.CurrentRows < 1 {
		t.Fatalf("inventory: %+v", body)
	}
}

func TestProviderCachePurgeAPI(t *testing.T) {
	reader := &blockingReader{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	settings := map[model.ProviderID]model.ProviderSettings{
		"lg": {ID: "lg", Label: "LG", MinChannels: 1, RefreshInterval: time.Hour},
	}
	reg := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{"lg": reader},
		settings,
	)
	lgFeed, _ := reg.Feed("lg")
	lgFeed.Set(provider.Lineup{FetchedAt: time.Now()})
	cc := cache.New(t.TempDir())
	if err := cc.CommitProvider("lg", provider.Raw{"schedule.json": []byte("RAW")}, cache.M3U("CUR"), cache.XMLTV("<tv/>"), provider.Meta{}); err != nil {
		t.Fatal(err)
	}
	gens := filepath.Join(mustCacheRoot(t, cc), "lg", "generations")
	extra := filepath.Join(gens, "extra-old")
	if err := os.MkdirAll(extra, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extra, "playlist.m3u"), []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := refresh.New(nil, reg, nil, cc, nil, nil, nil, nil, nil)
	runCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		select {
		case <-reader.release:
		default:
			close(reader.release)
		}
		time.Sleep(50 * time.Millisecond)
	})
	svc.Run(runCtx)

	h := server.ProviderCachePurgeHandler(svc, runCtx)
	req := httptest.NewRequest(http.MethodPost, "/api/providers/lg/cache/purge", nil)
	req.SetPathValue("id", "lg")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d %s", rec.Code, rec.Body.String())
	}
	select {
	case <-reader.started:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh did not start")
	}
	m3u, err := cc.ReadM3U("lg")
	if err != nil || string(m3u) != "CUR" {
		t.Fatalf("serving gen lost: %q %v", m3u, err)
	}
	if _, err := os.Stat(extra); !os.IsNotExist(err) {
		t.Fatal("extra gen still present")
	}

	// Second purge while refresh in flight → 409
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d", rec2.Code)
	}
}

func TestLogosClearAPI(t *testing.T) {
	cc := cache.New(t.TempDir())
	if err := cc.WriteLogo("lg", "ch1.png", []byte("png")); err != nil {
		t.Fatal(err)
	}
	settings := map[model.ProviderID]model.ProviderSettings{
		"lg": {ID: "lg", Label: "LG", MinChannels: 1, RefreshInterval: time.Hour},
	}
	reg := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{"lg": &blockingReader{
			started: make(chan struct{}),
			release: make(chan struct{}),
		}},
		settings,
	)
	svc := refresh.New(nil, reg, nil, cc, nil, nil, nil, nil, nil)
	h := server.LogosClearHandler(svc, context.Background())
	req := httptest.NewRequest(http.MethodDelete, "/api/logos/lg", nil)
	req.SetPathValue("provider", "lg")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	if _, err := cc.StatLogo("lg", "ch1.png"); err == nil {
		t.Fatal("logo still present")
	}
}

func mustCacheRoot(t *testing.T, cc *cache.Cache) string {
	t.Helper()
	path, err := cc.LogoPath("lg", "x.png")
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(path)))
}
