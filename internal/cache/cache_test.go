package cache_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

func TestProviderRoundTrip(t *testing.T) {
	cc := cache.New(t.TempDir())
	meta := provider.Meta{
		FetchedAt:               time.Now().UTC().Truncate(time.Second),
		Classifications:         map[string]model.Classification{"a": model.ClassNative, "b": model.ClassDRM},
		SyntheticChannelNumbers: provider.ChannelNumberAssignments{"a": 5000, "gone": 5001},
	}
	if err := cc.CommitProvider("lg", provider.Raw{"schedule.json": []byte("RAW")}, cache.M3U("#EXTM3U\n"), cache.XMLTV("<tv></tv>"), meta); err != nil {
		t.Fatal(err)
	}

	if m, err := cc.ReadM3U("lg"); err != nil || string(m) != "#EXTM3U\n" {
		t.Fatalf("ReadM3U: %q %v", m, err)
	}
	if x, err := cc.ReadXMLTV("lg"); err != nil || string(x) != "<tv></tv>" {
		t.Fatalf("ReadXMLTV: %q %v", x, err)
	}
	if raw, err := cc.ReadRaw("lg"); err != nil || string(raw["schedule.json"]) != "RAW" {
		t.Fatalf("ReadRaw: %q %v", raw, err)
	}
	got, ok := cc.LoadMeta("lg")
	if !ok || !got.FetchedAt.Equal(meta.FetchedAt) || got.Classifications["b"] != model.ClassDRM ||
		got.SyntheticChannelNumbers["gone"] != 5001 {
		t.Fatalf("LoadMeta: %+v ok=%v", got, ok)
	}
}

func TestRawRoundTrip(t *testing.T) {
	cc := cache.New(t.TempDir())
	if _, err := cc.ReadRaw("lg"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing raw want fs.ErrNotExist, got %v", err)
	}
	if err := cc.CommitProvider("lg", provider.Raw{"schedule.json": []byte("RAW-BYTES")}, nil, nil, provider.Meta{}); err != nil {
		t.Fatal(err)
	}
	got, err := cc.ReadRaw("lg")
	if err != nil || string(got["schedule.json"]) != "RAW-BYTES" {
		t.Fatalf("ReadRaw: %q %v", got, err)
	}
}

func TestMultipartRawRoundTrip(t *testing.T) {
	cc := cache.New(t.TempDir())
	want := provider.Raw{
		"channels.json.gz": []byte("CHANNELS"),
		"guide.xml.gz":     []byte("GUIDE"),
	}
	if err := cc.CommitProvider(model.ProviderPluto, want, nil, nil, provider.Meta{}); err != nil {
		t.Fatal(err)
	}
	got, err := cc.ReadRaw(model.ProviderPluto)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range want {
		if string(got[name]) != string(data) {
			t.Errorf("%s: got %q want %q", name, got[name], data)
		}
	}
}

func TestReadMissingIsNotExist(t *testing.T) {
	cc := cache.New(t.TempDir())
	if _, err := cc.ReadM3U("lg"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("want fs.ErrNotExist, got %v", err)
	}
	if _, err := cc.ReadAggregateM3U(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("aggregate want fs.ErrNotExist, got %v", err)
	}
}

func TestRawAndAggregateRoundTrip(t *testing.T) {
	cc := cache.New(t.TempDir())
	if err := cc.CommitProvider("lg", provider.Raw{"schedule.json": []byte("RAW")}, nil, nil, provider.Meta{}); err != nil {
		t.Fatal(err)
	}
	if err := cc.WriteAggregate(cache.M3U("#EXTM3U\n"), cache.XMLTV("<tv/>")); err != nil {
		t.Fatal(err)
	}
	if m, err := cc.ReadAggregateM3U(); err != nil || string(m) != "#EXTM3U\n" {
		t.Fatalf("aggregate m3u: %q %v", m, err)
	}
	if x, err := cc.ReadAggregateXMLTV(); err != nil || string(x) != "<tv/>" {
		t.Fatalf("aggregate xml: %q %v", x, err)
	}
}

func TestStatusRoundTrip(t *testing.T) {
	cc := cache.New(t.TempDir())
	want := provider.Status{
		LastAttemptAt: time.Now().UTC().Truncate(time.Second),
		LastError:     "upstream unavailable",
		LastErrorAt:   time.Now().UTC().Truncate(time.Second),
	}
	if err := cc.WriteStatus("lg", want); err != nil {
		t.Fatal(err)
	}
	got, ok := cc.LoadStatus("lg")
	if !ok || !got.LastAttemptAt.Equal(want.LastAttemptAt) || got.LastError != want.LastError {
		t.Fatalf("LoadStatus: %+v ok=%v", got, ok)
	}
}

func TestLegacyFlatLayoutFallback(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "lg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "raw"), []byte("LEGACY"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := []byte(`{"fetched_at":"2026-01-01T00:00:00Z"}`)
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), meta, 0o644); err != nil {
		t.Fatal(err)
	}
	raw, _, legacy, err := cache.New(root).LoadProvider("lg")
	if err != nil || !legacy || string(raw[provider.LegacyRaw]) != "LEGACY" {
		t.Fatalf("LoadProvider: raw=%q legacy=%v err=%v", raw, legacy, err)
	}
}

func TestSingleRawGenerationLoadsAndNextCommitMigrates(t *testing.T) {
	root := t.TempDir()
	generation := filepath.Join(root, "lg", "generations", "old")
	if err := os.MkdirAll(generation, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"raw":       "LEGACY",
		"meta.json": `{}`,
	} {
		if err := os.WriteFile(filepath.Join(generation, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "lg", "current"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cc := cache.New(root)
	raw, _, legacyLayout, err := cc.LoadProvider("lg")
	if err != nil || legacyLayout || string(raw[provider.LegacyRaw]) != "LEGACY" {
		t.Fatalf("load old generation: raw=%q legacy=%v err=%v", raw, legacyLayout, err)
	}
	if err := cc.CommitProvider("lg", provider.Raw{"schedule.json": []byte("NEW")}, nil, nil, provider.Meta{}); err != nil {
		t.Fatal(err)
	}
	raw, err = cc.ReadRaw("lg")
	if err != nil || string(raw["schedule.json"]) != "NEW" {
		t.Fatalf("migrated raw: %q %v", raw, err)
	}
	if _, exists := raw[provider.LegacyRaw]; exists {
		t.Fatalf("legacy raw remained after commit: %q", raw)
	}
}

func TestMalformedCurrentDoesNotFallBackToLegacy(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "lg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "raw"), []byte("STALE"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "current"), []byte("../bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.New(root).ReadRaw("lg"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("malformed current should not expose legacy data: %v", err)
	}
}

func TestUnselectedGenerationIsNeverVisible(t *testing.T) {
	root := t.TempDir()
	cc := cache.New(root)
	if err := cc.CommitProvider("lg", provider.Raw{"schedule.json": []byte("OLD")}, cache.M3U("OLD-M3U"), cache.XMLTV("OLD-XML"), provider.Meta{}); err != nil {
		t.Fatal(err)
	}

	orphan := filepath.Join(root, "lg", "generations", "interrupted")
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"raw":          "NEW",
		"playlist.m3u": "NEW-M3U",
		"guide.xml":    "NEW-XML",
		"meta.json":    `{}`,
	} {
		if err := os.WriteFile(filepath.Join(orphan, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	raw, err := cc.ReadRaw("lg")
	if err != nil || string(raw["schedule.json"]) != "OLD" {
		t.Fatalf("uncommitted generation became visible: %q %v", raw, err)
	}
	m3u, _ := cc.ReadM3U("lg")
	xml, _ := cc.ReadXMLTV("lg")
	if string(m3u) != "OLD-M3U" || string(xml) != "OLD-XML" {
		t.Fatalf("mixed generation: m3u=%q xml=%q", m3u, xml)
	}
}

func TestTraversalRejected(t *testing.T) {
	cc := cache.New(t.TempDir())
	if _, err := cc.ReadM3U("../evil"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("traversal should be rejected as not-exist, got %v", err)
	}
	for _, name := range []string{"", "../evil", "dir/file", `dir\file`, ".", ".."} {
		if err := cc.CommitProvider("lg", provider.Raw{name: []byte("RAW")}, nil, nil, provider.Meta{}); err == nil {
			t.Errorf("raw filename %q should be rejected", name)
		}
	}
	if err := cc.CommitProvider("lg", nil, nil, nil, provider.Meta{}); err == nil {
		t.Error("empty raw snapshot should be rejected")
	}
}
