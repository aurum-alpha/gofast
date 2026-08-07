package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/j27-aurum/gofast/internal/model"
)

func TestServeDemuxStableSlotsFull(t *testing.T) {
	t.Setenv("FASTPROXY_DEMUX_STABLE_MAX", "0")
	origin := NewStaticOrigin()
	origin.Set(model.ProviderPluto, "ch1", ChannelOrigin{
		StreamURL: "https://cdn.example/live.m3u8", Classification: model.ClassNative,
	})
	h := NewHandler(origin, NewStore(), nil)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/stable/pluto/ch1.ts", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestServeDemuxStableNativePipe(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "ffmpeg")
	script := "#!/bin/sh\nprintf 'Gfake-mpegts-bytes-here!!!!'\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FASTPROXY_FFMPEG", stub)
	t.Setenv("FASTPROXY_DEMUX_STABLE_MAX", "2")

	origin := NewStaticOrigin()
	origin.Set(model.ProviderPluto, "news", ChannelOrigin{
		StreamURL: "https://cdn.example/live.m3u8", Classification: model.ClassNative,
	})
	h := NewHandler(origin, NewStore(), nil)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/stable/pluto/news.ts", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "video/mp2t" {
		t.Fatalf("Content-Type=%q", ct)
	}
	if !strings.Contains(rec.Body.String(), "fake-mpegts") {
		t.Fatalf("body=%q", rec.Body.String())
	}
}

func TestResolveDemuxIngestAmagiLoopback(t *testing.T) {
	h := NewHandler(NewStaticOrigin(), NewStore(), nil)
	h.LoopbackBase = "http://127.0.0.1:8181"
	u, hdr, err := h.resolveDemuxIngest(context.Background(), model.ProviderLG, "x", ChannelOrigin{
		StreamURL:      "https://amagi.example/beacon.m3u8",
		Classification: model.ClassAmagiSSAI,
	})
	if err != nil {
		t.Fatal(err)
	}
	if u != "http://127.0.0.1:8181/stream/lg/x.m3u8" {
		t.Fatalf("url=%q", u)
	}
	if hdr != nil {
		t.Fatalf("loopback should not forward upstream headers to ffmpeg, got %v", hdr)
	}
}

func TestDemuxStableSnapshotRows(t *testing.T) {
	t.Setenv("FASTPROXY_DEMUX_STABLE_MAX", "4")
	tr := newDemuxStableTracker()
	id, slot, ok := tr.acquire(model.ProviderPluto, "a")
	if !ok {
		t.Fatal("acquire")
	}
	slot.bytesOut.Store(1000)
	slot.state.Store("streaming")
	active, max, sessions := tr.snapshotRows()
	if active != 1 || max != 4 || len(sessions) != 1 {
		t.Fatalf("active=%d max=%d sessions=%+v", active, max, sessions)
	}
	if sessions[0].ChannelID != "a" || sessions[0].BytesOut != 1000 {
		t.Fatalf("%+v", sessions[0])
	}
	tr.release(id)
	active, _, sessions = tr.snapshotRows()
	if active != 0 || len(sessions) != 0 {
		t.Fatalf("after release active=%d sessions=%+v", active, sessions)
	}
}

func TestServeDemuxStableOriginMiss(t *testing.T) {
	h := NewHandler(NewStaticOrigin(), NewStore(), nil)
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/stable/pluto/missing.ts", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d", rec.Code)
	}
}
