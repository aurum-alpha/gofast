package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/server"
)

func TestPlaylistHandlers(t *testing.T) {
	cc := cache.New(t.TempDir())
	if err := cc.WriteProvider("lg", cache.M3U("#EXTM3U\n"), cache.XMLTV("<tv></tv>"), provider.Meta{}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{file}", server.PlaylistFile(cc, nil))

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

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing.m3u", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope.txt", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 for unknown suffix without fallback, got %d", rec.Code)
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

	if err := cc.WriteAggregate(cache.M3U("#EXTM3U\nAGG\n"), cache.XMLTV("<tv/>")); err != nil {
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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{file}", server.PlaylistFile(cc, spa))

	// A single-segment SPA route (e.g. hard reload of /guide) must serve the app.
	for _, path := range []string{"/guide", "/providers", "/config"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != "INDEX" {
			t.Fatalf("%s: want SPA fallback, got %d %q", path, rec.Code, rec.Body.String())
		}
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing.m3u", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("playlist should not fall through: got %d", rec.Code)
	}
}
