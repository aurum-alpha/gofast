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

func TestClassifyAmagiSSAI(t *testing.T) {
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
	if got != model.ClassAmagiSSAI {
		t.Fatalf("got %q want AMAGI_SSAI", got)
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
	// Missing media extension before ? → AMAGI_SSAI even without /beacon/.
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
	if got != model.ClassAmagiSSAI {
		t.Fatalf("got %q want AMAGI_SSAI", got)
	}
}

func TestClassifyByURLSession(t *testing.T) {
	c := New(httpx.NewClient(0, 1), 1)
	url := "https://dai.google.com/linear/hls/event/abc123/master.m3u8"
	got := c.Classify(context.Background(), url)
	if got != model.ClassSession {
		t.Fatalf("got %q want SESSION", got)
	}
}

func TestClassifyByURLSession404StillSession(t *testing.T) {
	// Heuristic must win without probing; DistroTV masters often 404.
	var gets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gets.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(httpx.NewClient(0, 1), 1)
	// Use real DAI host shape (not httptest) so classifyByURL matches.
	got := c.Classify(context.Background(), "https://dai.google.com/linear/hls/event/x/master.m3u8")
	if got != model.ClassSession {
		t.Fatalf("got %q want SESSION", got)
	}
	if gets.Load() != 0 {
		t.Fatalf("SESSION must not probe, gets=%d", gets.Load())
	}
	_ = srv
}

func TestClassifyByURLXumoSSAI(t *testing.T) {
	c := New(httpx.NewClient(0, 1), 1)
	url := "https://d1bl6tskrpq9ze.cloudfront.net/hls/master.m3u8?ads.xumo_channelId=99992260&ads.channelId=ch1"
	got := c.Classify(context.Background(), url)
	if got != model.ClassXumoSSAI {
		t.Fatalf("got %q want XUMO_SSAI", got)
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

func TestClassifyChannelsSendsProviderHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRangeGET(t, r)
		if got := r.Header.Get("user-agent"); got != "okhttp/4.12.0" {
			t.Errorf("user-agent = %q", got)
		}
		_, _ = io.WriteString(w, "#EXTM3U\n#EXTINF:1.0,\na.ts\n")
	}))
	defer server.Close()

	client := New(httpx.NewClient(0, 1), 1)
	channels := client.ClassifyChannels(context.Background(), []model.Channel{{
		StreamURL:      server.URL + "/master.m3u8",
		RequestHeaders: map[string]string{"user-agent": "okhttp/4.12.0"},
	}})
	if channels[0].Classification != model.ClassNative {
		t.Fatalf("classification: %+v", channels[0])
	}
}

func TestIsAmagiSSAIURI(t *testing.T) {
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
		if got := isAmagiSSAIURI(tt.in); got != tt.want {
			t.Errorf("isAmagiSSAIURI(%q)=%v want %v", tt.in, got, tt.want)
		}
	}
}

func TestClassifyByURLTable(t *testing.T) {
	tests := []struct {
		in      string
		want    model.Classification
		matched bool
	}{
		{"https://dai.google.com/linear/hls/event/e1/master.m3u8", model.ClassSession, true},
		{"https://foo.dai.google.com/linear/hls/event/e1/master.m3u8", model.ClassSession, true},
		{"https://dai.google.com/other/path.m3u8", "", false},
		{"https://cdn.example/hls/master.m3u8?ads.channelId=1", model.ClassXumoSSAI, true},
		{"https://cdn.example/hls/master.m3u8?ADS.XUMO_CHANNELID=1", model.ClassXumoSSAI, true},
		{"https://cdn.example/hls/master.m3u8?token=1", "", false},
		{"not-a-url", "", false},
	}
	for _, tt := range tests {
		got, ok := classifyByURL(tt.in)
		if ok != tt.matched || got != tt.want {
			t.Errorf("classifyByURL(%q)=(%q,%v) want (%q,%v)", tt.in, got, ok, tt.want, tt.matched)
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
