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

func TestHandlerBeaconE2E(t *testing.T) {
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

	origin := NewStaticOrigin()
	origin.Set(model.ProviderLG, "news", ChannelOrigin{
		StreamURL:      upstream.URL + "/master.m3u8",
		Classification: model.ClassAmagiSSAI,
	})
	store := NewStore()
	rep := NewReporter("", store)
	h := NewHandler(origin, store, rep)
	// Force playlist/segment clients through head-failing default transport via custom servers only.
	h.segments.http.Transport = headFailTransport{rt: http.DefaultTransport}

	mux := http.NewServeMux()
	h.Register(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stream/lg/news.m3u8", nil)
	req.Host = "proxy.test"
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("stream status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	lines := nonCommentURILines(body)
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "http://proxy.test/seg/") {
		t.Fatalf("rewritten media lines=%v body=%s", lines, body)
	}
	segURL := lines[0]
	// Hit segment via handler path.
	path := strings.TrimPrefix(segURL, "http://proxy.test")
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, path, nil)
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK || rec2.Body.String() != "TSDATA" {
		t.Fatalf("seg status=%d body=%q", rec2.Code, rec2.Body.String())
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
