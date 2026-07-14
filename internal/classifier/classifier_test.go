package classifier

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
)

func TestClassifyAmagiBeacon(t *testing.T) {
	var sawHEAD atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/master.m3u8", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			sawHEAD.Store(true)
		}
		assertRangeGET(t, r)
		_, _ = io.WriteString(w, `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=800000
media.m3u8
`)
	})
	mux.HandleFunc("/media.m3u8", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			sawHEAD.Store(true)
		}
		assertRangeGET(t, r)
		_, _ = io.WriteString(w, `#EXTM3U
#EXTINF:6.0,
https://cdn.example/beacon/track?x=1
#EXTINF:6.0,
https://cdn.example/beacon/track?x=2
#EXTINF:6.0,
https://cdn.example/beacon/track?x=3
`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(httpx.NewClient(0, 1), 2)
	got := c.Classify(context.Background(), srv.URL+"/master.m3u8")
	if got != model.ClassBeacon {
		t.Fatalf("got %q want BEACON", got)
	}
	if sawHEAD.Load() {
		t.Fatal("HEAD must never be used")
	}
}

func TestClassifyCleanNative(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/master.m3u8", func(w http.ResponseWriter, r *http.Request) {
		assertRangeGET(t, r)
		_, _ = io.WriteString(w, `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=800000
media.m3u8
`)
	})
	mux.HandleFunc("/media.m3u8", func(w http.ResponseWriter, r *http.Request) {
		assertRangeGET(t, r)
		_, _ = io.WriteString(w, `#EXTM3U
#EXTINF:6.0,
seg001.ts
#EXTINF:6.0,
seg002.ts?token=abc
#EXTINF:6.0,
https://cdn.example/a/seg003.m4s
`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(httpx.NewClient(0, 1), 2)
	got := c.Classify(context.Background(), srv.URL+"/master.m3u8")
	if got != model.ClassNative {
		t.Fatalf("got %q want NATIVE", got)
	}
}

func TestClassifyRelativeSegmentURLs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/live/master.m3u8", func(w http.ResponseWriter, r *http.Request) {
		assertRangeGET(t, r)
		_, _ = io.WriteString(w, `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=500000
../media/index.m3u8
`)
	})
	mux.HandleFunc("/media/index.m3u8", func(w http.ResponseWriter, r *http.Request) {
		assertRangeGET(t, r)
		_, _ = io.WriteString(w, `#EXTM3U
#EXTINF:4.0,
./seg/1.ts
#EXTINF:4.0,
./seg/2.ts
`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(httpx.NewClient(0, 1), 2)
	got := c.Classify(context.Background(), srv.URL+"/live/master.m3u8")
	if got != model.ClassNative {
		t.Fatalf("got %q want NATIVE", got)
	}
}

func TestClassifyFetchErrorKeepsNative(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRangeGET(t, r)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	c := New(httpx.NewClient(0, 1), 2)
	got := c.Classify(context.Background(), srv.URL+"/missing.m3u8")
	if got != model.ClassNative {
		t.Fatalf("got %q want NATIVE on fetch error", got)
	}
}

func TestClassifyExtensionlessWithoutBeaconPath(t *testing.T) {
	// Missing media extension before ? → BEACON even without /beacon/.
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		assertRangeGET(t, r)
		if strings.HasSuffix(r.URL.Path, "media.m3u8") {
			_, _ = io.WriteString(w, `#EXTM3U
#EXTINF:6.0,
https://ads.example/track/abc123?foo=1
`)
			return
		}
		_, _ = io.WriteString(w, `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=1
media.m3u8
`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(httpx.NewClient(0, 1), 1)
	got := c.Classify(context.Background(), srv.URL+"/master.m3u8")
	if got != model.ClassBeacon {
		t.Fatalf("got %q want BEACON", got)
	}
}

func TestClassifyChannelsSkipsDRMAndUsesPool(t *testing.T) {
	var gets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gets.Add(1)
		assertRangeGET(t, r)
		_, _ = io.WriteString(w, `#EXTM3U
#EXTINF:1.0,
a.ts
`)
	}))
	defer srv.Close()

	chs := []model.Channel{
		{ID: "drm", StreamURL: srv.URL + "/x.m3u8", Classification: model.ClassDRM},
		{ID: "a", StreamURL: srv.URL + "/a.m3u8"},
		{ID: "b", StreamURL: srv.URL + "/b.m3u8"},
	}
	c := New(httpx.NewClient(0, 1), 2)
	out := c.ClassifyChannels(context.Background(), chs)
	if out[0].Classification != model.ClassDRM {
		t.Fatalf("DRM mutated: %q", out[0].Classification)
	}
	if out[1].Classification != model.ClassNative || out[2].Classification != model.ClassNative {
		t.Fatalf("%+v", out)
	}
	if gets.Load() < 2 {
		t.Fatalf("expected probes for non-DRM channels, gets=%d", gets.Load())
	}
}

func TestIsBeaconURI(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"https://x/beacon/y", true},
		{"https://x/seg/1.ts", false},
		{"https://x/seg/1.ts?q=1", false},
		{"https://x/track/abc?q=1", true},
		{"seg.aac", false},
		{"seg.mp4", false},
		{"seg.m4s", false},
	}
	for _, tt := range tests {
		if got := isBeaconURI(tt.in); got != tt.want {
			t.Errorf("isBeaconURI(%q)=%v want %v", tt.in, got, tt.want)
		}
	}
}

func assertRangeGET(t *testing.T, r *http.Request) {
	t.Helper()
	if r.Method != http.MethodGet {
		t.Fatalf("method %s", r.Method)
	}
	if r.Header.Get("Range") == "" {
		t.Fatal("missing Range header")
	}
}
