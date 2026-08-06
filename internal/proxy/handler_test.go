package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/j27-aurum/gofast/internal/model"
)

type headFailTransport struct {
	rt http.RoundTripper
}

func (t headFailTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodHead {
		panic("HEAD must not be issued")
	}
	return t.rt.RoundTrip(req)
}

// TestBeaconPlaylistIntegration is the J27-29 accept path: live httptest upstream
// + proxy servers, Amagi/legacy-BEACON playlist rewrite, then segment fetch.
func TestBeaconPlaylistIntegration(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			t.Fatal("HEAD to upstream")
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/master.m3u8"):
			_, _ = io.WriteString(w, `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:6
#EXTINF:6.0,
/beacon/track?x=1
`)
		case strings.Contains(r.URL.Path, "/beacon/"):
			http.Redirect(w, r, "/media/seg.ts", http.StatusFound)
		case strings.HasSuffix(r.URL.Path, "/seg.ts"):
			_, _ = io.WriteString(w, "TSDATA")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	for _, class := range []model.Classification{model.ClassAmagiSSAI, "BEACON"} {
		t.Run(string(class), func(t *testing.T) {
			origin := NewStaticOrigin()
			origin.Set(model.ProviderLG, "news", ChannelOrigin{
				StreamURL:      upstream.URL + "/master.m3u8",
				Classification: class,
			})
			store := NewStore()
			rep := NewReporter("", store)
			h := NewHandler(origin, store, rep)
			h.segments.http.Transport = headFailTransport{rt: http.DefaultTransport}

			mux := http.NewServeMux()
			h.Register(mux)
			proxySrv := httptest.NewServer(mux)
			t.Cleanup(proxySrv.Close)

			streamURL := proxySrv.URL + "/stream/lg/news.m3u8"
			resp, err := http.Get(streamURL)
			if err != nil {
				t.Fatalf("GET stream: %v", err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read stream: %v", err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("stream status=%d body=%s", resp.StatusCode, body)
			}
			lines := nonCommentURILines(string(body))
			if len(lines) != 1 || !strings.HasPrefix(lines[0], proxySrv.URL+"/seg/") {
				t.Fatalf("rewritten media lines=%v body=%s", lines, body)
			}

			segResp, err := http.Get(lines[0])
			if err != nil {
				t.Fatalf("GET seg: %v", err)
			}
			defer segResp.Body.Close()
			segBody, err := io.ReadAll(segResp.Body)
			if err != nil {
				t.Fatalf("read seg: %v", err)
			}
			if segResp.StatusCode != http.StatusOK || string(segBody) != "TSDATA" {
				t.Fatalf("seg status=%d body=%q", segResp.StatusCode, segBody)
			}
		})
	}
}

func TestHandlerNative302(t *testing.T) {
	origin := NewStaticOrigin()
	origin.Set(model.ProviderLG, "sports", ChannelOrigin{
		StreamURL:      "https://cdn.example/native.m3u8",
		Classification: model.ClassNative,
	})
	h := NewHandler(origin, NewStore(), nil)
	mux := http.NewServeMux()
	h.Register(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stream/lg/sports.m3u8", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status=%d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://cdn.example/native.m3u8" {
		t.Fatalf("location=%s", loc)
	}
}

func TestHandlerNativeBrowserRewrite(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = io.WriteString(w, `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:6
#EXTINF:6.0,
segment0.ts
`)
	}))
	t.Cleanup(upstream.Close)

	origin := NewStaticOrigin()
	origin.Set(model.ProviderPluto, "CA_news", ChannelOrigin{
		StreamURL:      upstream.URL + "/master.m3u8",
		Classification: model.ClassNative,
	})
	h := NewHandler(origin, NewStore(), nil)
	mux := http.NewServeMux()
	h.Register(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stream/pluto/CA_news.m3u8?browser=1", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("ACAO=%q", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "/seg/") {
		t.Fatalf("expected rewritten seg URI, body=%s", body)
	}
	if strings.Contains(body, "segment0.ts") {
		t.Fatalf("upstream segment should be rewritten: %s", body)
	}
}

// J27-75: configured PublicBase wins over plain-HTTP inbound (TLS edge without
// X-Forwarded-Proto) so rewritten segment URIs stay on the public HTTPS origin.
func TestHandlerPublicBaseOverridesRequestScheme(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:6
#EXTINF:6.0,
/beacon/track?x=1
`)
	}))
	t.Cleanup(upstream.Close)

	origin := NewStaticOrigin()
	origin.Set(model.ProviderLG, "news", ChannelOrigin{
		StreamURL:      upstream.URL + "/master.m3u8",
		Classification: model.ClassAmagiSSAI,
	})
	h := NewHandler(origin, NewStore(), nil)
	h.PublicBase = "https://fast-proxy.example.com"

	mux := http.NewServeMux()
	h.Register(mux)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8181/stream/lg/news.m3u8", nil)
	req.Host = "127.0.0.1:8181"
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	lines := nonCommentURILines(body)
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "https://fast-proxy.example.com/seg/") {
		t.Fatalf("rewritten lines=%v body=%s", lines, body)
	}
	if strings.Contains(body, "http://127.0.0.1") || strings.Contains(body, "http://fast-proxy") {
		t.Fatalf("must not emit request-derived http URIs: %s", body)
	}
}

// J27-64: under proxy_all, XUMO_SSAI uses ProxyNone 302 with ads.* query intact
// (not Amagi rewrite). Hell's Kitchen–shaped CloudFront URL.
func TestHandlerXumoSSAI302PreservesAdsQuery(t *testing.T) {
	upstream := "https://d1bl6tskrpq9ze.cloudfront.net/hls/master.m3u8?ads.xumo_channelId=99992260&ads.channelId=99992260"
	origin := NewStaticOrigin()
	origin.Set(model.ProviderLG, "99992260", ChannelOrigin{
		StreamURL:      upstream,
		Classification: model.ClassXumoSSAI,
	})
	h := NewHandler(origin, NewStore(), nil)
	mux := http.NewServeMux()
	h.Register(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stream/lg/99992260.m3u8", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != upstream {
		t.Fatalf("location=%q want %q", loc, upstream)
	}
	if body := rec.Body.String(); strings.Contains(body, "#EXTM3U") {
		t.Fatal("XUMO must 302, not return a rewritten playlist body")
	}
}
