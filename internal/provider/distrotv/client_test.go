package distrotv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

func TestParseFeedLive(t *testing.T) {
	raw := []byte(`{
		"shows": {
			"1": {
				"type": "live",
				"title": "Demo News",
				"img_logo": "https://a.example/logo.png",
				"genre": "News,English",
				"seasons": [{"episodes": [{"id": 95226, "content": {"url": "https://cdn.example/a.m3u8?ads.rnd=__CACHE_BUSTER__"}}]}]
			},
			"2": {
				"type": "vod",
				"title": "Skip Me",
				"seasons": [{"episodes": [{"id": "x", "content": {"url": "https://cdn.example/b.m3u8"}}]}]
			}
		}
	}`)
	shows, err := ParseFeedLive(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(shows) != 1 || shows[0].ID != "95226" || shows[0].Name != "Demo News" || shows[0].Group != "News" {
		t.Fatalf("shows=%+v", shows)
	}
}

func TestSanitizeURLMacros(t *testing.T) {
	in := "https://cdn.example/master.m3u8?ads.rnd=__CACHE_BUSTER__&ads.deviceId=__DEVICE_ID__&junk=__UNKNOWN_MACRO__"
	out := SanitizeURL(in)
	if strings.Contains(out, "__CACHE_BUSTER__") || strings.Contains(out, "__DEVICE_ID__") || strings.Contains(out, "__UNKNOWN_MACRO__") {
		t.Fatalf("macros remain: %s", out)
	}
	if !strings.Contains(out, "ads.rnd=") || !strings.Contains(out, "ads.deviceId=") {
		t.Fatalf("expected substituted params: %s", out)
	}
}

func TestOpaqueRoundTrip(t *testing.T) {
	id := JoinChannelID("qq", "95226")
	if id != "QQ_95226" {
		t.Fatalf("id=%q", id)
	}
	u := OpaqueStreamURL(id)
	got, ok := ParseOpaque(u)
	if !ok || got != id {
		t.Fatalf("ParseOpaque(%q)=%q ok=%v", u, got, ok)
	}
	geo, raw := SplitChannelID(id, DefaultGeo)
	if geo != "QQ" || raw != "95226" {
		t.Fatalf("split=%s/%s", geo, raw)
	}
}

func TestClientParse(t *testing.T) {
	c := New(DefaultSettings(), nil)
	raw := provider.Raw{
		RawFeed: []byte(`{"shows":{"1":{"type":"live","title":"CCTV","genre":"News","seasons":[{"episodes":[{"id":"23094","content":{"url":"https://global.cgtn.cicc.media.caton.cloud/master/x.m3u8"}}]}]}}}`),
	}
	chs, _, err := c.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(chs) != 1 {
		t.Fatalf("len=%d", len(chs))
	}
	ch := chs[0]
	if ch.Classification != model.ClassDistroResolve {
		t.Fatalf("class=%s", ch.Classification)
	}
	if !strings.HasPrefix(ch.StreamURL, OpaqueSchemePrefix) {
		t.Fatalf("stream=%s", ch.StreamURL)
	}
	if ch.RequestHeaders["Origin"] != "https://distro.tv" {
		t.Fatalf("headers=%v", ch.RequestHeaders)
	}
}

func TestResolverResolve(t *testing.T) {
	feed := `{"shows":{"1":{"type":"live","title":"X","seasons":[{"episodes":[{"id":"99","content":{"url":"https://live-mcl.cdn01.net/a.m3u8?ads.rnd=__CACHE_BUSTER__"}}]}]}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(feed))
	}))
	t.Cleanup(srv.Close)

	r := NewResolver(httpx.NewClient(5*time.Second, 1), srv.URL+"?type=live", AndroidUA)
	got, err := r.Resolve(context.Background(), OpaqueStreamURL("QQ_99"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "https://live-mcl.cdn01.net/a.m3u8") || strings.Contains(got, "__CACHE_BUSTER__") {
		t.Fatalf("got %s", got)
	}
}

func TestNeedsPlaylistProxy(t *testing.T) {
	if !NeedsPlaylistProxy("https://global.cgtn.cicc.media.caton.cloud/master/x.m3u8") {
		t.Fatal("caton should proxy")
	}
	if !NeedsPlaylistProxy("https://amg01314-x.playouts.now.amagi.tv/playlist.m3u8") {
		t.Fatal("amagi should proxy")
	}
	if NeedsPlaylistProxy("https://live-mcl.cdn01.net/a.m3u8") {
		t.Fatal("plain CDN should 302")
	}
}

func TestDefaultSettingsDisabled(t *testing.T) {
	s := DefaultSettings()
	if s.IsEnabled() {
		t.Fatal("DistroTV should default disabled")
	}
	if s.Region != DefaultGeo || s.MinChannels != 1 {
		t.Fatalf("defaults=%+v", s)
	}
}
