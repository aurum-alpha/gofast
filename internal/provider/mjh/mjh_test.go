package mjh

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/xmltv"
)

func TestPlutoMissingMetadataSlugUsesDefault(t *testing.T) {
	client := New(Source{
		ID:          model.ProviderPluto,
		Directory:   "PlutoTV",
		DefaultSlug: "plu-{id}.m3u8",
	}, model.ProviderSettings{ID: model.ProviderPluto, Region: "us"}, nil)
	channels, programmes, err := client.Parse(fixtureRaw(t, "pluto"))
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 1 || channels[0].StreamURL != "https://jmp2.uk/plu-pluto-news.m3u8" {
		t.Fatalf("channels: %+v", channels)
	}
	if channels[0].RequestHeaders["User-Agent"] != "okhttp/4.9.0" || channels[0].RequestHeaders["X-Region"] != "us" {
		t.Fatalf("headers: %+v", channels[0].RequestHeaders)
	}
	if len(programmes) != 1 || programmes[0].Title != "Pluto Headlines" {
		t.Fatalf("programmes: %+v", programmes)
	}
}

func TestPlutoProgrammeIconsAreStrippedOnReEmission(t *testing.T) {
	client := New(Source{
		ID:          model.ProviderPluto,
		Directory:   "PlutoTV",
		DefaultSlug: "plu-{id}.m3u8",
	}, model.ProviderSettings{ID: model.ProviderPluto, Region: "us"}, nil)
	channels, programmes, err := client.Parse(fixtureRaw(t, "pluto"))
	if err != nil {
		t.Fatal(err)
	}
	for index := range channels {
		channels[index].Normalize()
	}

	data, err := xmltv.Marshal(channels, programmes, "Pluto")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Channels []struct {
			Icons []struct{} `xml:"icon"`
		} `xml:"channel"`
		Programmes []struct {
			Icons []struct{} `xml:"icon"`
		} `xml:"programme"`
	}
	if err := xml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Channels) != 1 || len(document.Channels[0].Icons) != 1 {
		t.Fatalf("channel icon was not retained: %s", data)
	}
	for _, programme := range document.Programmes {
		if len(programme.Icons) != 0 {
			t.Fatalf("programme icon leaked into output: %s", data)
		}
	}
}

func TestSamsungSlugHeadersAndDRM(t *testing.T) {
	client := New(Source{
		ID:        model.ProviderSamsung,
		Directory: "SamsungTVPlus",
	}, model.ProviderSettings{ID: model.ProviderSamsung, Region: "us"}, nil)
	channels, programmes, err := client.Parse(fixtureRaw(t, "samsung"))
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 2 || len(programmes) != 2 {
		t.Fatalf("channels=%d programmes=%d", len(channels), len(programmes))
	}
	if channels[0].StreamURL != "https://jmp2.uk/stvp-samsung-drm" || channels[1].StreamURL != "https://jmp2.uk/stvp-samsung-news" {
		t.Fatalf("slug URLs: %+v", channels)
	}
	var drm model.Channel
	for _, channel := range channels {
		if channel.ID == "samsung-drm" {
			drm = channel
		}
		if channel.RequestHeaders["User-Agent"] != "okhttp/4.12.0" {
			t.Fatalf("headers: %+v", channel.RequestHeaders)
		}
	}
	if drm.Classification != model.ClassDRM || drm.LicenseURL == "" || drm.Number != 1111 {
		t.Fatalf("DRM channel: %+v", drm)
	}
}

func TestRokuRegionlessShape(t *testing.T) {
	client := New(Source{
		ID:          model.ProviderRoku,
		Directory:   "Roku",
		Regionless:  true,
		DefaultSlug: "rok-{id}.m3u8",
	}, model.ProviderSettings{ID: model.ProviderRoku}, nil)
	channels, programmes, err := client.Parse(fixtureRaw(t, "roku"))
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 1 || channels[0].Group != "News" || channels[0].StreamURL != "https://jmp2.uk/rok-roku-news.m3u8" {
		t.Fatalf("channels: %+v", channels)
	}
	if len(programmes) != 1 || programmes[0].Title != "Roku Live" {
		t.Fatalf("programmes: %+v", programmes)
	}
	if client.guideURL != "https://i.mjh.nz/Roku/all.xml.gz" {
		t.Fatalf("guide URL: %s", client.guideURL)
	}
}

func TestMissingSlugFails(t *testing.T) {
	client := New(Source{ID: "test", Directory: "Test"}, model.ProviderSettings{ID: "test", Region: "us"}, nil)
	_, _, err := client.Parse(fixtureRaw(t, "pluto"))
	if err == nil || !strings.Contains(err.Error(), "missing slug") {
		t.Fatalf("error: %v", err)
	}
}

func TestMalformedGzipAndXMLFail(t *testing.T) {
	client := New(Source{ID: "test", Regionless: true, DefaultSlug: "x-{id}"}, model.ProviderSettings{ID: "test"}, nil)
	if _, _, err := client.Parse(provider.Raw{RawChannels: []byte("bad"), RawGuide: []byte("bad")}); err == nil {
		t.Fatal("malformed metadata gzip should fail")
	}
	raw := fixtureRaw(t, "roku")
	raw[RawGuide] = gzipBytes(t, []byte("<tv><programme>"))
	if _, _, err := client.Parse(raw); err == nil {
		t.Fatal("malformed XML should fail")
	}
}

func TestFetchGetsBothPayloadsAndAppliesMetadataHeaders(t *testing.T) {
	fixture := fixtureRaw(t, "samsung")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-config") != "configured" {
			t.Errorf("%s missing configured header", r.URL.Path)
		}
		switch r.URL.Path {
		case "/channels":
			_, _ = w.Write(fixture[RawChannels])
		case "/guide":
			if got := r.Header.Get("user-agent"); got != "configured-agent" {
				t.Errorf("guide user-agent = %q", got)
			}
			_, _ = w.Write(fixture[RawGuide])
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	settings := model.ProviderSettings{
		ID:          model.ProviderSamsung,
		Region:      "us",
		ChannelsURL: server.URL + "/channels",
		EPGURL:      server.URL + "/guide",
		Headers: map[string]string{
			"user-agent": "configured-agent",
			"x-config":   "configured",
		},
	}
	client := New(Source{ID: model.ProviderSamsung}, settings, httpx.NewClient(time.Second, 1))
	raw, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(raw[RawChannels]) == 0 || len(raw[RawGuide]) == 0 {
		t.Fatalf("raw parts: %+v", raw)
	}
}

func TestFetchGuideFailureReturnsNoPartialRaw(t *testing.T) {
	fixture := fixtureRaw(t, "pluto")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/channels" {
			_, _ = w.Write(fixture[RawChannels])
			return
		}
		http.Error(w, "guide unavailable", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)

	settings := model.ProviderSettings{
		ID:          model.ProviderPluto,
		Region:      "us",
		ChannelsURL: server.URL + "/channels",
		EPGURL:      server.URL + "/guide",
	}
	client := New(Source{ID: model.ProviderPluto, DefaultSlug: "plu-{id}.m3u8"}, settings, httpx.NewClient(time.Second, 1))
	raw, err := client.Fetch(context.Background())
	if err == nil || raw != nil {
		t.Fatalf("raw=%v err=%v", raw, err)
	}
}

func fixtureRaw(t *testing.T, name string) provider.Raw {
	t.Helper()
	channels, err := os.ReadFile(filepath.Join("testdata", name+".channels.json"))
	if err != nil {
		t.Fatal(err)
	}
	guide, err := os.ReadFile(filepath.Join("testdata", name+".xml"))
	if err != nil {
		t.Fatal(err)
	}
	return provider.Raw{
		RawChannels: gzipBytes(t, channels),
		RawGuide:    gzipBytes(t, guide),
	}
}

func gzipBytes(t *testing.T, data []byte) []byte {
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
