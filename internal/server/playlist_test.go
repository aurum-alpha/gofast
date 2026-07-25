package server_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/server"
)

// playlistTestRegistry wires enabled feeds for lg and pluto (no cache for pluto).
func playlistTestRegistry() *provider.Registry {
	settings := map[model.ProviderID]model.ProviderSettings{
		model.ProviderLG:    {ID: model.ProviderLG, Enabled: boolPtr(true)},
		model.ProviderPluto: {ID: model.ProviderPluto, Enabled: boolPtr(true)},
	}
	return provider.NewRegistry(map[model.ProviderID]provider.Reader{
		model.ProviderLG:    healthStubReader{},
		model.ProviderPluto: healthStubReader{},
	}, settings)
}

func TestPlaylistHandlers(t *testing.T) {
	cc := cache.New(t.TempDir())
	if err := cc.CommitProvider("lg", provider.Raw{"schedule.json": []byte("RAW")}, cache.M3U("#EXTM3U\n"), cache.XMLTV("<tv></tv>"), provider.Meta{}); err != nil {
		t.Fatal(err)
	}
	reg := playlistTestRegistry()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{file}", server.PlaylistFile(reg, cc, nil))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/lg.m3u", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "#EXTM3U\n" {
		t.Fatalf("m3u: %d %q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/lg.xml", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<tv>") {
		t.Fatalf("xml: %d %q", rec.Code, rec.Body.String())
	}

	// Enabled provider with no cached files yet: not ready.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pluto.m3u", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}

	// Unknown provider: 404 even with a .m3u suffix.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing.m3u", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 for unknown provider, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope.txt", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 for unknown suffix without fallback, got %d", rec.Code)
	}
}

func TestPlaylistDisabledProviderIs404(t *testing.T) {
	cc := cache.New(t.TempDir())
	// Cached last-good files exist on disk...
	if err := cc.CommitProvider("lg", provider.Raw{"schedule.json": []byte("RAW")}, cache.M3U("#EXTM3U\n"), cache.XMLTV("<tv></tv>"), provider.Meta{}); err != nil {
		t.Fatal(err)
	}
	// ...but the provider was disabled live (feed removed from the registry).
	reg := playlistTestRegistry()
	disabled := false
	reg.Remove(model.ProviderLG, model.ProviderSettings{ID: model.ProviderLG, Enabled: &disabled})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{file}", server.PlaylistFile(reg, cc, nil))

	for _, path := range []string{"/lg.m3u", "/lg.xml"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: want 404 while disabled, got %d", path, rec.Code)
		}
	}
}

func TestAggregateHandlers(t *testing.T) {
	cc := cache.New(t.TempDir())

	// Not ready before the aggregator runs.
	rec := httptest.NewRecorder()
	server.AggregatePlaylist(cc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/playlist.m3u", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 before generation, got %d", rec.Code)
	}

	if err := cc.CommitAggregate(cache.M3U("#EXTM3U\nAGG\n"), cache.XMLTV("<tv/>")); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	server.AggregatePlaylist(cc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/playlist.m3u", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "AGG") {
		t.Fatalf("aggregate m3u: %d %q", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	server.AggregateGuide(cc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/epg.xml", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<tv/>") {
		t.Fatalf("aggregate xml: %d %q", rec.Code, rec.Body.String())
	}
}

func TestPlaylistFallsBackToSPA(t *testing.T) {
	cc := cache.New(t.TempDir())
	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("INDEX"))
	})
	reg := playlistTestRegistry()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{file}", server.PlaylistFile(reg, cc, spa))

	// A single-segment SPA route (e.g. hard reload of /guide) must serve the app.
	for _, path := range []string{"/guide", "/providers", "/config"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != "INDEX" {
			t.Fatalf("%s: want SPA fallback, got %d %q", path, rec.Code, rec.Body.String())
		}
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pluto.m3u", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("playlist should not fall through: got %d", rec.Code)
	}
}

func TestPlaylistETag304(t *testing.T) {
	body := []byte("#EXTM3U\n#EXTINF:-1,Test\nhttp://example/stream\n")
	sum := sha256.Sum256(body)
	wantETag := `"` + hex.EncodeToString(sum[:]) + `"`

	cc := cache.New(t.TempDir())
	if err := cc.CommitProvider("lg", provider.Raw{"schedule.json": []byte("RAW")}, cache.M3U(body), cache.XMLTV("<tv></tv>"), provider.Meta{}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{file}", server.PlaylistFile(playlistTestRegistry(), cc, nil))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/lg.m3u", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("first GET: %d", rec.Code)
	}
	if got := rec.Header().Get("ETag"); got != wantETag {
		t.Fatalf("ETag: got %q want %q", got, wantETag)
	}
	if rec.Header().Get("Content-Type") != "application/vnd.apple.mpegurl" {
		t.Fatalf("Content-Type: %q", rec.Header().Get("Content-Type"))
	}

	req := httptest.NewRequest(http.MethodGet, "/lg.m3u", nil)
	req.Header.Set("If-None-Match", wantETag)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("If-None-Match: want 304, got %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("304 body should be empty, got %q", rec.Body.String())
	}
	if got := rec.Header().Get("ETag"); got != wantETag {
		t.Fatalf("304 ETag: got %q want %q", got, wantETag)
	}

	req = httptest.NewRequest(http.MethodGet, "/lg.m3u", nil)
	req.Header.Set("If-None-Match", `"deadbeef"`)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != string(body) {
		t.Fatalf("mismatched ETag should return 200 body: %d %q", rec.Code, rec.Body.String())
	}
}

func TestAggregateETag304(t *testing.T) {
	body := []byte("#EXTM3U\nAGG\n")
	sum := sha256.Sum256(body)
	wantETag := `"` + hex.EncodeToString(sum[:]) + `"`

	cc := cache.New(t.TempDir())
	if err := cc.CommitAggregate(cache.M3U(body), cache.XMLTV("<tv/>")); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	server.AggregatePlaylist(cc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/playlist.m3u", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("ETag") != wantETag {
		t.Fatalf("aggregate: %d etag=%q", rec.Code, rec.Header().Get("ETag"))
	}

	req := httptest.NewRequest(http.MethodGet, "/playlist.m3u", nil)
	req.Header.Set("If-None-Match", wantETag)
	rec = httptest.NewRecorder()
	server.AggregatePlaylist(cc).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("aggregate If-None-Match: want 304, got %d", rec.Code)
	}
}
