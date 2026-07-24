package lg

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/httpx"
)

func TestDefaultSettingsRefreshHorizon(t *testing.T) {
	s := DefaultSettings()
	if s.RefreshInterval != 3*time.Hour {
		t.Fatalf("RefreshInterval = %v want 3h", s.RefreshInterval)
	}
	if s.ExpectedGuideHorizon != 12*time.Hour {
		t.Fatalf("ExpectedGuideHorizon = %v want 12h", s.ExpectedGuideHorizon)
	}
}

func TestParseScheduleFixture(t *testing.T) {
	path := filepath.Join("testdata", "schedulelist.json")
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	chs, progs, err := ParseSchedule(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(chs) != 2 {
		t.Fatalf("channels: got %d want 2 (deduped; malformed skipped)", len(chs))
	}
	news := chs[0]
	if news.ID != "ch-news" || news.Name != "News One" || news.Number != 10 || news.Group != "News" {
		t.Fatalf("news: %+v", news)
	}
	if news.StreamURL != "https://stream.example/news.m3u8" {
		t.Fatalf("query not stripped: %q", news.StreamURL)
	}
	if news.LogoURL != "https://cdn.example/logo-news.png" {
		t.Fatalf("logo: %q", news.LogoURL)
	}

	dup := chs[1]
	if dup.ID != "ch-dup" || dup.Name != "Dup A" || dup.Number != 20 {
		t.Fatalf("first-seen wins for channel meta: %+v", dup)
	}
	if dup.StreamURL != "https://stream.example/dup.m3u8" {
		t.Fatalf("dup stream: %q", dup.StreamURL)
	}

	if len(progs) != 3 {
		t.Fatalf("programmes: got %d want 3 (merged across categories)", len(progs))
	}
	if progs[0].Title != "Morning Report" || !progs[0].Start.Equal(time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("prog0: %+v", progs[0])
	}
	if progs[0].Stop.Sub(progs[0].Start) != time.Hour {
		t.Fatalf("duration: %v", progs[0].Stop.Sub(progs[0].Start))
	}
	if progs[1].ChannelID != "ch-dup" || progs[1].Title != "Episode 1" {
		t.Fatalf("prog1: %+v", progs[1])
	}
	if progs[2].Title != "Episode 2" {
		t.Fatalf("prog2: %+v", progs[2])
	}
}

func TestParseScheduleBase64ZlibFixture(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "schedulelist.b64z"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(body), "eN") {
		t.Fatalf("fixture should look like base64 zlib, got %q", body[:8])
	}
	chs, progs, err := ParseSchedule(strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	if len(chs) != 2 || len(progs) != 3 {
		t.Fatalf("chs=%d progs=%d", len(chs), len(progs))
	}
}

func TestFetchNormalizesBase64ZlibToJSON(t *testing.T) {
	wire, err := os.ReadFile(filepath.Join("testdata", "schedulelist.b64z"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain;charset=UTF-8")
		_, _ = w.Write(wire)
	}))
	t.Cleanup(srv.Close)

	settings := DefaultSettings()
	settings.ChannelsURL = srv.URL
	client := New(settings, httpx.NewClient(5*time.Second, 0))
	raw, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	body := raw[rawSchedule]
	if len(body) == 0 || body[0] != '{' {
		t.Fatalf("Fetch should store JSON, got %q", truncate(body, 40))
	}
	chs, _, err := client.Parse(raw)
	if err != nil || len(chs) != 2 {
		t.Fatalf("Parse after Fetch: chs=%d err=%v", len(chs), err)
	}
}

func TestDecodeScheduleRejectsGarbage(t *testing.T) {
	if _, err := decodeScheduleBytes([]byte("not-json-or-b64")); err == nil {
		t.Fatal("expected error")
	}
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
