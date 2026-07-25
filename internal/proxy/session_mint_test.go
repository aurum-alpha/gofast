package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/j27-aurum/gofast/internal/model"
)

func TestDAIEventID(t *testing.T) {
	got, err := daiEventID("https://dai.google.com/linear/hls/event/AbCd123/master.m3u8")
	if err != nil || got != "AbCd123" {
		t.Fatalf("got %q err=%v", got, err)
	}
	got, err = daiEventID("https://foo.dai.google.com/linear/hls/event/xyz/stream/1/master.m3u8")
	if err != nil || got != "xyz" {
		t.Fatalf("subdomain got %q err=%v", got, err)
	}
	if _, err := daiEventID("https://cdn.example/linear/hls/event/x/master.m3u8"); err == nil {
		t.Fatal("expected non-dai host error")
	}
	if _, err := daiEventID("https://dai.google.com/ondemand/hls/content/x/vid/y/master.m3u8"); err == nil {
		t.Fatal("expected missing event path error")
	}
}

func TestSessionMintIntegration(t *testing.T) {
	var posts int
	dai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			t.Fatal("HEAD to DAI")
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/linear/v1/hls/event/Evt1/stream") {
			posts++
			if ct := r.Header.Get("Content-Type"); !strings.Contains(ct, "application/x-www-form-urlencoded") {
				t.Fatalf("content-type=%q", ct)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{
				"stream_manifest": "https://dai.google.com/linear/hls/pa/event/Evt1/stream/live/master.m3u8",
				"stream_id":       "live:ATL",
			})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(dai.Close)

	origin := NewStaticOrigin()
	origin.Set(model.ProviderID("distrotv"), "ch1", ChannelOrigin{
		StreamURL:      "https://dai.google.com/linear/hls/event/Evt1/master.m3u8",
		Classification: model.ClassSession,
	})
	store := NewStore()
	h := NewHandler(origin, store, nil)
	h.mint.base = dai.URL

	mux := http.NewServeMux()
	h.Register(mux)
	proxySrv := httptest.NewServer(mux)
	t.Cleanup(proxySrv.Close)

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(proxySrv.URL + "/stream/distrotv/ch1.m3u8")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "/stream/live/master.m3u8") {
		t.Fatalf("location=%s", loc)
	}
	if posts != 1 {
		t.Fatalf("posts=%d want 1", posts)
	}

	resp2, err := client.Get(proxySrv.URL + "/stream/distrotv/ch1.m3u8")
	if err != nil {
		t.Fatalf("GET2: %v", err)
	}
	defer resp2.Body.Close()
	_, _ = io.Copy(io.Discard, resp2.Body)
	if resp2.StatusCode != http.StatusFound || posts != 1 {
		t.Fatalf("cached status=%d posts=%d", resp2.StatusCode, posts)
	}
}

func TestSessionMintDoesNotRewrite(t *testing.T) {
	origin := NewStaticOrigin()
	origin.Set(model.ProviderLG, "news", ChannelOrigin{
		StreamURL:      "https://dai.google.com/linear/hls/event/E/master.m3u8",
		Classification: model.ClassSession,
	})
	h := NewHandler(origin, NewStore(), nil)
	dai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"stream_manifest": "https://cdn.example/live.m3u8",
		})
	}))
	t.Cleanup(dai.Close)
	h.mint.base = dai.URL

	mux := http.NewServeMux()
	h.Register(mux)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stream/lg/news.m3u8", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "https://cdn.example/live.m3u8" {
		t.Fatalf("location=%s", loc)
	}
	if strings.Contains(rec.Body.String(), "#EXT") {
		t.Fatal("SESSION must not return rewritten playlist body")
	}
}
