package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
	if check.HTTPStatus != 200 && check.HTTPStatus != 206 {
		t.Fatalf("expected http status 200/206, got %d", check.HTTPStatus)
	}
}

func TestSegmentProberHTTP403(t *testing.T) {
	html := "<!DOCTYPE html><html><body>denied forever</body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(html))
	}))
	t.Cleanup(srv.Close)

	p := &SegmentProber{HTTP: httpx.NewClient(5*time.Second, 1)}
	check := p.Check(context.Background(), model.Channel{StreamURL: srv.URL + "/x.m3u8"})
	if check.Result != model.HealthCheckFailure || check.FailureClass != "http_403" {
		t.Fatalf("got %+v", check)
	}
	if check.HTTPStatus != 403 {
		t.Fatalf("expected http_status 403, got %d", check.HTTPStatus)
	}
	if !strings.Contains(check.Detail, "HTTP 403") {
		t.Fatalf("detail missing status: %q", check.Detail)
	}
	if !strings.Contains(check.Detail, "denied forever") {
		t.Fatalf("detail missing full body: %q", check.Detail)
	}
	if !strings.Contains(check.Detail, "Content-Type: text/html") {
		t.Fatalf("detail missing content-type: %q", check.Detail)
	}
}

func TestSegmentProberUsesEmittedURL(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/upstream.m3u8", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not hit upstream when EmittedURL is set")
	})
	mux.HandleFunc("/emitted.m3u8", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:6,\nseg.ts\n"))
	})
	mux.HandleFunc("/seg.ts", func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 188)
		body[0] = 0x47
		_, _ = w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	p := &SegmentProber{HTTP: httpx.NewClient(5*time.Second, 1)}
	check := p.Check(context.Background(), model.Channel{
		StreamURL:  srv.URL + "/upstream.m3u8",
		EmittedURL: srv.URL + "/emitted.m3u8",
	})
	if check.Result != model.HealthCheckSuccess {
		t.Fatalf("got %+v detail=%q", check, check.Detail)
	}
	if check.DurationMs <= 0 || check.BytesRead < 188 {
		t.Fatalf("expected duration/bytes metadata, got %+v", check)
	}
}

func TestSegmentProberSoftRetryOn503(t *testing.T) {
	var hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/media.m3u8", func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("busy"))
			return
		}
		_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:6,\nseg.ts\n"))
	})
	mux.HandleFunc("/seg.ts", func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 188)
		body[0] = 0x47
		_, _ = w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	p := &SegmentProber{HTTP: httpx.NewClient(5*time.Second, 1), SoftRetries: 1}
	check := p.Check(context.Background(), model.Channel{StreamURL: srv.URL + "/media.m3u8"})
	if check.Result != model.HealthCheckSuccess {
		t.Fatalf("expected success after soft retry: %+v detail=%q", check, check.Detail)
	}
	if hits.Load() < 2 {
		t.Fatalf("expected retry, hits=%d", hits.Load())
	}
}

func TestSegmentProberRetriesWithoutRangeOn416(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/master.m3u8", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			_, _ = w.Write([]byte("no ranges"))
			return
		}
		_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:6,\nseg.ts\n"))
	})
	mux.HandleFunc("/seg.ts", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
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
	if check.HTTPStatus != 200 {
		t.Fatalf("expected final http 200 after 416 fallback, got %d", check.HTTPStatus)
	}
}

func TestSegmentProberDetailOnNetworkishFailure(t *testing.T) {
	p := &SegmentProber{HTTP: httpx.NewClient(time.Second, 1)}
	check := p.Check(context.Background(), model.Channel{
		StreamURL: "http://127.0.0.1:1/nope.m3u8",
	})
	if check.Result != model.HealthCheckFailure {
		t.Fatalf("got %+v", check)
	}
	if check.Detail == "" || check.FailureClass == "" {
		t.Fatalf("expected class+detail, got %+v", check)
	}
}

func TestSegmentProberAES128EncryptedOK(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/media.m3u8", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(
			"#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"https://keys.example/k\"\n#EXTINF:6,\nseg.ts\n",
		))
	})
	mux.HandleFunc("/seg.ts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/MP2T")
		w.WriteHeader(http.StatusPartialContent)
		// Ciphertext: no MPEG-TS sync byte.
		body := make([]byte, 1024)
		for i := range body {
			body[i] = byte(i*7 + 3)
		}
		_, _ = w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	p := &SegmentProber{HTTP: httpx.NewClient(5*time.Second, 1)}
	check := p.Check(context.Background(), model.Channel{
		StreamURL: srv.URL + "/media.m3u8",
	})
	if check.Result != model.HealthCheckSuccess {
		t.Fatalf("encrypted segment should pass L2: %+v detail=%q", check, check.Detail)
	}
	if check.HTTPStatus != 206 {
		t.Fatalf("expected 206, got %d", check.HTTPStatus)
	}
}

func TestSegmentOK(t *testing.T) {
	ts := make([]byte, 188)
	ts[0] = 0x47
	if !segmentOK(ts, "", false) {
		t.Fatal("mpeg-ts")
	}
	cipher := make([]byte, 512)
	for i := range cipher {
		cipher[i] = 0xab
	}
	if segmentOK(cipher, "", false) {
		t.Fatal("cipher without encryption flag or media CT should fail")
	}
	if !segmentOK(cipher, "", true) {
		t.Fatal("AES playlist: size alone is enough")
	}
	if !segmentOK(cipher, "video/MP2T", false) {
		t.Fatal("media content-type fallback")
	}
	if segmentOK([]byte("hi"), "video/MP2T", true) {
		t.Fatal("short body always fails")
	}
}

func TestLooksLikeMedia(t *testing.T) {
	ts := make([]byte, 188)
	ts[0] = 0x47
	if !looksLikeMedia(ts) {
		t.Fatal("mpeg-ts")
	}
	fmp4 := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	for len(fmp4) < 188 {
		fmp4 = append(fmp4, 0)
	}
	if !looksLikeMedia(fmp4) {
		t.Fatal("fmp4")
	}
	if looksLikeMedia([]byte("hi")) {
		t.Fatal("short")
	}
	offset := make([]byte, 400)
	offset[10] = 0x47
	offset[10+188] = 0x47
	if !looksLikeMedia(offset) {
		t.Fatal("offset sync")
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
		Classification: model.ClassAmagiSSAI,
		StreamURL:      "https://up/beacon",
		EmittedURL:     "http://proxy/stream/lg/x.m3u8",
	}
	if ProbeURL(ch) != ch.EmittedURL {
		t.Fatal(ProbeURL(ch))
	}
}

func TestRewriteProxyProbeURL(t *testing.T) {
	t.Parallel()
	pub := "http://localhost:8181"
	internal := "http://fastproxy:8181"
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{pub + "/stream/samsung/x.m3u8", internal + "/stream/samsung/x.m3u8"},
		{pub, internal},
		{"https://cdn.example/a.m3u8", "https://cdn.example/a.m3u8"},
		{pub + "/stream/x", internal + "/stream/x"},
	}
	for _, tc := range cases {
		got := RewriteProxyProbeURL(tc.in, pub, internal)
		if got != tc.want {
			t.Fatalf("RewriteProxyProbeURL(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
	if got := RewriteProxyProbeURL(pub+"/stream/x", pub, ""); got != pub+"/stream/x" {
		t.Fatalf("empty internal should leave URL: %q", got)
	}
}

func TestFFProbeCheckRewritesProxyURL(t *testing.T) {
	// Use a stub that fails immediately so we can assert FinalURL was rewritten
	// without needing a real stream (ffprobe exits non-zero on /bin/false).
	p := &FFProbe{
		Path:              "/bin/false",
		Timeout:           time.Second,
		ProxyPublicBase:   "http://localhost:8181",
		ProxyInternalBase: "http://fastproxy:8181",
	}
	check := p.Check(context.Background(), model.Channel{
		EmittedURL: "http://localhost:8181/stream/samsung/USBA300023G8.m3u8",
	})
	want := "http://fastproxy:8181/stream/samsung/USBA300023G8.m3u8"
	if check.FinalURL != want {
		t.Fatalf("FinalURL=%q want %q (failure_class=%s detail=%q)", check.FinalURL, want, check.FailureClass, check.Detail)
	}
}
