package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/j27-aurum/gofast/internal/server"
	"github.com/j27-aurum/gofast/internal/snapshot"
)

func TestPlaylistHandlers(t *testing.T) {
	store := snapshot.NewStore()
	store.Put(snapshot.Snapshot{
		ProviderID: "lg",
		M3U:        []byte("#EXTM3U\n"),
		XML:        []byte("<tv></tv>"),
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{file}", server.PlaylistFile(store, nil))

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

func TestPlaylistFallsBackToSPA(t *testing.T) {
	store := snapshot.NewStore()
	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("INDEX"))
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{file}", server.PlaylistFile(store, spa))

	// A single-segment SPA route (e.g. hard reload of /guide) must serve the app,
	// not 404 — while real playlists still resolve.
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
