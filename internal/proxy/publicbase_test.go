package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublicBaseFromRequest(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8181/stream/lg/x.m3u8", nil)
	req.Host = "127.0.0.1:8181"
	if got := publicBaseFromRequest(req); got != "http://127.0.0.1:8181" {
		t.Fatalf("plain request: got %q", got)
	}

	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "fast-proxy.example.com")
	if got := publicBaseFromRequest(req); got != "https://fast-proxy.example.com" {
		t.Fatalf("forwarded: got %q", got)
	}

	req.Header.Set("X-Forwarded-Proto", "https, http")
	req.Header.Set("X-Forwarded-Host", "a.example, b.example")
	if got := publicBaseFromRequest(req); got != "https://a.example" {
		t.Fatalf("comma list: got %q", got)
	}
}

func TestResolvePublicBasePrefersConfigured(t *testing.T) {
	t.Parallel()
	h := &Handler{PublicBase: "https://fast-proxy.example.com"}
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8181/stream/lg/x.m3u8", nil)
	req.Host = "127.0.0.1:8181"
	// Simulate TLS-terminating edge that forgot X-Forwarded-Proto.
	if got := h.resolvePublicBase(req); got != "https://fast-proxy.example.com" {
		t.Fatalf("configured win: got %q", got)
	}

	h.PublicBase = ""
	if got := h.resolvePublicBase(req); got != "http://127.0.0.1:8181" {
		t.Fatalf("fallback to request: got %q", got)
	}
}
