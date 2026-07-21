package published

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/xmltv"
)

const fixturePlaylist = `#EXTM3U
#EXTINF:-1 tvg-id="news one" tvg-name="News" group-title="Local",News
https://example.test/news.m3u8
`

const fixtureGuide = `<tv>
  <channel id="news_one"/>
  <programme channel="news_one" start="20260721120000 +0000" stop="20260721130000 +0000">
    <title>News & Weather</title>
  </programme>
</tv>`

func TestFetchReturnsExactPairAndAppliesHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "configured-agent" {
			t.Errorf("%s user-agent = %q", r.URL.Path, got)
		}
		if got := r.Header.Get("X-Test"); got != "yes" {
			t.Errorf("%s X-Test = %q", r.URL.Path, got)
		}
		switch r.URL.Path {
		case "/playlist":
			_, _ = w.Write([]byte(fixturePlaylist))
		case "/guide":
			_, _ = w.Write([]byte(fixtureGuide))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	settings := model.ProviderSettings{
		ID:        "test",
		M3UURL:    server.URL + "/playlist",
		EPGURL:    server.URL + "/guide",
		UserAgent: "default-agent",
		Headers: map[string]string{
			"user-agent": "configured-agent",
			"x-test":     "yes",
		},
	}
	client := New(Source{ID: "test"}, settings, httpx.NewClient(time.Second, 1))
	raw, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(raw[RawPlaylist]) != fixturePlaylist || string(raw[RawGuide]) != fixtureGuide {
		t.Fatalf("raw: %+v", raw)
	}
}

func TestFetchGuideFailureReturnsNoPartialRaw(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/playlist" {
			_, _ = w.Write([]byte(fixturePlaylist))
			return
		}
		http.Error(w, "unavailable", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)
	settings := model.ProviderSettings{ID: "test", M3UURL: server.URL + "/playlist", EPGURL: server.URL + "/guide"}
	client := New(Source{ID: "test"}, settings, httpx.NewClient(time.Second, 1))
	raw, err := client.Fetch(context.Background())
	if err == nil || raw != nil {
		t.Fatalf("raw=%v err=%v", raw, err)
	}
}

func TestParseGzipGuideAndPropagatesHeaders(t *testing.T) {
	settings := model.ProviderSettings{
		ID:        "test",
		UserAgent: "browser",
		Headers:   map[string]string{"x-test": "yes"},
	}
	client := New(Source{ID: "test", EPGGzip: true}, settings, nil)
	channels, programmes, err := client.Parse(provider.Raw{
		RawPlaylist:  []byte(fixturePlaylist),
		RawGuideGzip: gzipFixture(t, []byte(fixtureGuide)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 1 || len(programmes) != 1 {
		t.Fatalf("channels=%d programmes=%d", len(channels), len(programmes))
	}
	if channels[0].RequestHeaders["User-Agent"] != "browser" || channels[0].RequestHeaders["X-Test"] != "yes" {
		t.Fatalf("headers: %+v", channels[0].RequestHeaders)
	}
	if programmes[0].Title != "News & Weather" {
		t.Fatalf("programme: %+v", programmes[0])
	}
	matched, rate := guideMatch(channels, []string{"news_one", "orphan"})
	if matched != 1 || rate != 1 {
		t.Fatalf("match=%d rate=%f", matched, rate)
	}
}

func TestProviderFixtures(t *testing.T) {
	tests := []struct {
		name           string
		gzipGuide      bool
		wantChannels   int
		wantProgrammes int
		wantMatched    int
		wantRate       float64
		wantFirstID    string
		wantFirstTitle string
		groupPrefix    string
		wantFirstGroup string
	}{
		{"xumo", true, 2, 2, 2, 1, "99991247", "NBC News", "", "News"},
		{"distrotv", true, 2, 2, 2, 1, "dtv_EPGACE TV", "Rock & Roll & More", "", "Entertainment"},
		{"localnow", false, 2, 2, 1, 0.5, "LN_NEWS", "Local Headlines", "LocalNow🇺🇸:", "My City"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			playlist := readFixture(t, test.name+".m3u")
			guide := readFixture(t, test.name+".xml")
			source := Source{ID: model.ProviderID(test.name), EPGGzip: test.gzipGuide, GroupPrefix: test.groupPrefix}
			raw := provider.Raw{RawPlaylist: playlist}
			if test.gzipGuide {
				raw[RawGuideGzip] = gzipFixture(t, guide)
			} else {
				raw[RawGuide] = guide
			}
			client := New(source, model.ProviderSettings{ID: source.ID}, nil)
			channels, programmes, err := client.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			if len(channels) != test.wantChannels || len(programmes) != test.wantProgrammes {
				t.Fatalf("channels=%d programmes=%d", len(channels), len(programmes))
			}
			if channels[0].ID != test.wantFirstID || programmes[0].Title != test.wantFirstTitle {
				t.Fatalf("first channel=%+v programme=%+v", channels[0], programmes[0])
			}
			if channels[0].Group != test.wantFirstGroup {
				t.Fatalf("first group=%q want %q", channels[0].Group, test.wantFirstGroup)
			}
			documentReader, closeGuide, err := openGuide(raw[client.guideRawName()])
			if err != nil {
				t.Fatal(err)
			}
			document, err := xmltv.Parse(documentReader)
			closeGuide()
			if err != nil {
				t.Fatal(err)
			}
			matched, rate := guideMatch(channels, document.ChannelIDs)
			if matched != test.wantMatched || rate != test.wantRate {
				t.Fatalf("match=%d rate=%f", matched, rate)
			}
		})
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func gzipFixture(t *testing.T, data []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
