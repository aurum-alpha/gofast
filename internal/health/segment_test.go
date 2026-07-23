package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
)

func TestSegmentProberSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/master.m3u8", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			t.Fatal("HEAD forbidden")
		}
		_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000\nmedia.m3u8\n"))
	})
	mux.HandleFunc("/media.m3u8", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:6,\nseg.ts\n"))
	})
	mux.HandleFunc("/seg.ts", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Range"), "bytes=") {
			t.Fatalf("expected Range, got %q", r.Header.Get("Range"))
		}
		body := make([]byte, 188)
		body[0] = 0x47
		_, _ = w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	p := &SegmentProber{HTTP: httpx.NewClient(5*time.Second, 1)}
	check := p.Check(context.Background(), model.Channel{
		StreamURL: srv.URL + "/master.m3u8",
	})
	if check.Result != model.HealthCheckSuccess {
		t.Fatalf("got %+v", check)
	}
}

func TestSegmentProberHTTP403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	p := &SegmentProber{HTTP: httpx.NewClient(5*time.Second, 1)}
	check := p.Check(context.Background(), model.Channel{StreamURL: srv.URL + "/x.m3u8"})
	if check.Result != model.HealthCheckFailure || check.FailureClass == "" {
		t.Fatalf("got %+v", check)
	}
}

func TestLooksLikeMedia(t *testing.T) {
	ts := make([]byte, 188)
	ts[0] = 0x47
	if !looksLikeMedia(ts) {
		t.Fatal("mpeg-ts")
	}
	fmp4 := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	// pad to min
	for len(fmp4) < 188 {
		fmp4 = append(fmp4, 0)
	}
	if !looksLikeMedia(fmp4) {
		t.Fatal("fmp4")
	}
	if looksLikeMedia([]byte("hi")) {
		t.Fatal("short")
	}
}

func TestFFProbeEmptyURL(t *testing.T) {
	p := &FFProbe{Path: "/bin/false", Timeout: time.Second}
	check := p.Check(context.Background(), model.Channel{})
	if check.Result != model.HealthCheckFailure || check.FailureClass != "no_url" {
		t.Fatalf("got %+v", check)
	}
}

func TestProbeURLBeaconUsesEmitted(t *testing.T) {
	ch := model.Channel{
		Classification: model.ClassBeacon,
		StreamURL:      "https://up/beacon",
		EmittedURL:     "http://proxy/stream/lg/x.m3u8",
	}
	if ProbeURL(ch) != ch.EmittedURL {
		t.Fatal(ProbeURL(ch))
	}
}
