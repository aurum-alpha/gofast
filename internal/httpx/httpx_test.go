package httpx_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/j27-aurum/gofast/internal/httpx"
)

func TestRangedGetUsesGETWithRangeNotHEAD(t *testing.T) {
	var method atomic.Value
	method.Store("")
	var gotRange string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method.Store(r.Method)
		gotRange = r.Header.Get("Range")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	client := httpx.NewClient(0, 1)
	resp, err := client.RangedGet(context.Background(), srv.URL, 0, 1023)
	if err != nil {
		t.Fatalf("RangedGet: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if method.Load() != http.MethodGet {
		t.Fatalf("method = %q, want GET", method.Load())
	}
	if gotRange != "bytes=0-1023" {
		t.Fatalf("Range = %q, want bytes=0-1023", gotRange)
	}
}

func TestDoRejectsHEAD(t *testing.T) {
	client := httpx.NewClient(0, 1)
	req, err := http.NewRequest(http.MethodHead, "http://example.com/stream.m3u8", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Do(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for HEAD request")
	}
}
