package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// The UI is a build artifact copied into the container, so these tests drive
// the handler with a stand-in filesystem rather than requiring a vite build to
// have run. Whether the real bundle is correct is an integration concern,
// exercised against the built container.
func testUI() fstest.MapFS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(`<!doctype html><title>GoFAST</title><div id="root"></div>`),
		},
		"assets/index-abc123.js": &fstest.MapFile{Data: []byte("console.log(1)")},
	}
}

func get(t *testing.T, path string) (*http.Response, string) {
	t.Helper()
	s := httptest.NewServer(handlerFor(testUI()))
	t.Cleanup(s.Close)

	resp, err := http.Get(s.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(raw)
}

func TestSPAIndex(t *testing.T) {
	resp, body := get(t, "/")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(body, "GoFAST") {
		t.Fatalf("index missing GoFAST brand, body=%q", body)
	}
	if !strings.Contains(body, "root") {
		t.Fatalf("index missing root mount")
	}
}

func TestSPAFallback(t *testing.T) {
	resp, body := get(t, "/providers")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(body, "root") {
		t.Fatal("SPA fallback should serve index.html")
	}
}

func TestAssetServed(t *testing.T) {
	resp, body := get(t, "/assets/index-abc123.js")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(body, "console.log") {
		t.Fatalf("asset body = %q", body)
	}
}

// A missing hashed asset must 404 rather than silently serving index.html —
// otherwise a stale bundle reference returns HTML where JS is expected.
func TestMissingAssetNotFound(t *testing.T) {
	resp, _ := get(t, "/assets/index-stale.js")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
