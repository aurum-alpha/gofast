package cache_test

import (
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

func TestProviderRoundTrip(t *testing.T) {
	cc := cache.New(t.TempDir())
	meta := provider.Meta{
		FetchedAt:       time.Now().UTC().Truncate(time.Second),
		Classifications: map[string]model.Classification{"a": model.ClassNative, "b": model.ClassDRM},
	}
	if err := cc.WriteProvider("lg", cache.M3U("#EXTM3U\n"), cache.XMLTV("<tv></tv>"), meta); err != nil {
		t.Fatal(err)
	}

	if m, err := cc.ReadM3U("lg"); err != nil || string(m) != "#EXTM3U\n" {
		t.Fatalf("ReadM3U: %q %v", m, err)
	}
	if x, err := cc.ReadXMLTV("lg"); err != nil || string(x) != "<tv></tv>" {
		t.Fatalf("ReadXMLTV: %q %v", x, err)
	}
	got, ok := cc.LoadMeta("lg")
	if !ok || !got.FetchedAt.Equal(meta.FetchedAt) || got.Classifications["b"] != model.ClassDRM {
		t.Fatalf("LoadMeta: %+v ok=%v", got, ok)
	}
}

func TestRawRoundTrip(t *testing.T) {
	cc := cache.New(t.TempDir())
	if _, err := cc.ReadRaw("lg"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing raw want fs.ErrNotExist, got %v", err)
	}
	if err := cc.WriteRaw("lg", []byte("RAW-BYTES")); err != nil {
		t.Fatal(err)
	}
	got, err := cc.ReadRaw("lg")
	if err != nil || string(got) != "RAW-BYTES" {
		t.Fatalf("ReadRaw: %q %v", got, err)
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
	if err := cc.WriteRaw("lg", []byte("RAW")); err != nil {
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

func TestTraversalRejected(t *testing.T) {
	cc := cache.New(t.TempDir())
	if _, err := cc.ReadM3U("../evil"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("traversal should be rejected as not-exist, got %v", err)
	}
}
