package logocache

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/model"
)

func TestServeHTTPLazyMissAndFreshHit(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png-bytes"))
	}))
	t.Cleanup(srv.Close)

	store := cache.New(t.TempDir())
	c := New(store, srv.Client(), "http://base", time.Hour)
	t.Cleanup(c.Close)

	source := srv.URL + "/a.png"
	resolve := func(provider model.ProviderID, channelID string) (string, map[string]string, bool) {
		if provider == "lg" && channelID == "ch1" {
			return source, nil, true
		}
		return "", nil, false
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/logos/lg/ch1.png", nil)
	c.ServeHTTP(rec, req, "lg", "ch1.png", resolve)
	if rec.Code != http.StatusOK || rec.Body.String() != "png-bytes" {
		t.Fatalf("miss fill: status=%d body=%q", rec.Code, rec.Body.String())
	}
	if hits.Load() != 1 {
		t.Fatalf("hits=%d", hits.Load())
	}

	rec = httptest.NewRecorder()
	c.ServeHTTP(rec, req, "lg", "ch1.png", resolve)
	if rec.Code != http.StatusOK || hits.Load() != 1 {
		t.Fatalf("fresh: status=%d hits=%d", rec.Code, hits.Load())
	}
}

func TestServeHTTPSoftStaleRevalidates(t *testing.T) {
	var hits atomic.Int32
	var conditional atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("If-None-Match") == `"v1"` {
			conditional.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte("png-bytes"))
	}))
	t.Cleanup(srv.Close)

	store := cache.New(t.TempDir())
	c := New(store, srv.Client(), "http://base", time.Hour)
	t.Cleanup(c.Close)

	source := srv.URL + "/a.png"
	ch := model.Channel{Provider: "lg", NormalizedID: "ch1", LogoURL: source}
	if _, logoErr := c.Ensure(context.Background(), ch); logoErr != "" {
		t.Fatal(logoErr)
	}
	path, err := store.LogoPath("lg", "ch1.png")
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
	meta := c.readMeta("lg", "ch1.png")
	meta.FetchedAt = past
	if err := c.writeMeta("lg", "ch1.png", meta); err != nil {
		t.Fatal(err)
	}

	resolve := func(model.ProviderID, string) (string, map[string]string, bool) {
		return source, nil, true
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/logos/lg/ch1.png", nil)
	c.ServeHTTP(rec, req, "lg", "ch1.png", resolve)
	if rec.Code != http.StatusOK || rec.Body.String() != "png-bytes" {
		t.Fatalf("revalidate: status=%d body=%q", rec.Code, rec.Body.String())
	}
	if hits.Load() < 2 || conditional.Load() != 1 {
		t.Fatalf("hits=%d conditional=%d", hits.Load(), conditional.Load())
	}
}

func TestServeHTTPURLChangeHardInvalidates(t *testing.T) {
	var lastPath string
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		lastPath = r.URL.Path
		if r.Header.Get("If-None-Match") != "" {
			t.Errorf("hard invalidate must not send conditional headers")
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("body-" + r.URL.Path))
	}))
	t.Cleanup(srv.Close)

	store := cache.New(t.TempDir())
	c := New(store, srv.Client(), "http://base", time.Hour)
	t.Cleanup(c.Close)

	old := srv.URL + "/old.png"
	newURL := srv.URL + "/new.png"
	if _, logoErr := c.Ensure(context.Background(), model.Channel{
		Provider: "lg", NormalizedID: "ch1", LogoURL: old,
	}); logoErr != "" {
		t.Fatal(logoErr)
	}

	resolve := func(model.ProviderID, string) (string, map[string]string, bool) {
		return newURL, nil, true
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/logos/lg/ch1.png", nil)
	c.ServeHTTP(rec, req, "lg", "ch1.png", resolve)
	if rec.Code != http.StatusOK || lastPath != "/new.png" || hits != 2 {
		t.Fatalf("status=%d path=%q hits=%d body=%q", rec.Code, lastPath, hits, rec.Body.String())
	}
	if rec.Body.String() != "body-/new.png" {
		t.Fatalf("body=%q", rec.Body.String())
	}
}

func TestRewriteURLsNoHTTP(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c := New(cache.New(t.TempDir()), srv.Client(), "http://base", 0)
	t.Cleanup(c.Close)
	chs := []model.Channel{
		{Provider: "pluto", NormalizedID: "a", LogoURL: srv.URL + "/a.jpg"},
		{Provider: "pluto", NormalizedID: "b", LogoURL: ""},
	}
	c.RewriteURLs(chs)
	if hits != 0 {
		t.Fatalf("emit rewrite must not HTTP, hits=%d", hits)
	}
	if chs[0].LogoURL != "http://base/logos/pluto/a.jpg" {
		t.Fatalf("chs[0]=%q", chs[0].LogoURL)
	}
	if chs[0].LogoSourceURL != srv.URL+"/a.jpg" {
		t.Fatalf("source=%q", chs[0].LogoSourceURL)
	}
	if chs[1].LogoURL != "" {
		t.Fatalf("empty logo should stay empty")
	}
}
