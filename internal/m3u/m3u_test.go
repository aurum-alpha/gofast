package m3u_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/j27-aurum/gofast/internal/m3u"
	"github.com/j27-aurum/gofast/internal/model"
)

func TestMarshalProviderGolden(t *testing.T) {
	channels := providerChannels()
	before := append([]model.Channel(nil), channels...)
	got, err := m3u.Marshal(channels, "TEST_PROVIDER")
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "provider.golden.m3u", got)
	if !reflect.DeepEqual(channels, before) {
		t.Fatal("marshal mutated caller channels")
	}
}

func TestMarshalAggregateGoldenAndDeterministic(t *testing.T) {
	sources := []m3u.Source{
		{Provider: "fixture_a", Label: "FIXTURE_A", Channels: []model.Channel{
			{ID: "shared", NormalizedID: "shared", Name: "Fixture A Shared", OffsetNumber: 2002, StreamURL: "https://stream.test/fixture-a.m3u8"},
			{ID: "late", NormalizedID: "late", Name: "Fixture A Unnumbered", StreamURL: "https://stream.test/late.m3u8"},
		}},
		{Provider: "fixture_b", Label: "FIXTURE_B", Channels: []model.Channel{
			{ID: "shared", NormalizedID: "shared", Name: "Fixture B Shared", OffsetNumber: 1001, StreamURL: "https://stream.test/fixture-b.m3u8"},
		}},
	}

	got, err := m3u.MarshalAll(sources, m3u.Options{NamespaceIDs: true})
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "aggregate.golden.m3u", got)

	reversed := []m3u.Source{sources[1], sources[0]}
	reversed[1].Channels = []model.Channel{sources[0].Channels[1], sources[0].Channels[0]}
	again, err := m3u.MarshalAll(reversed, m3u.Options{NamespaceIDs: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(got) {
		t.Fatalf("output changed after input reorder:\n%s\n---\n%s", got, again)
	}
}

func TestMarshalRejectsDuplicateIDs(t *testing.T) {
	_, err := m3u.Marshal([]model.Channel{
		{ID: "a b", NormalizedID: "a_b", Name: "One", StreamURL: "https://stream.test/one.m3u8"},
		{ID: "a_b", NormalizedID: "a_b", Name: "Two", StreamURL: "https://stream.test/two.m3u8"},
	}, "Test")
	if err == nil {
		t.Fatal("expected duplicate emitted id error")
	}
}

func TestMarshalRejectsMultilinePlaybackURL(t *testing.T) {
	_, err := m3u.Marshal([]model.Channel{{
		ID:           "news",
		NormalizedID: "news",
		Name:         "News",
		StreamURL:    "https://stream.test/news.m3u8\n#EXTINF:-1,Injected",
	}}, "Test")
	if err == nil {
		t.Fatal("expected invalid playback URL error")
	}
}

func assertGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	want, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s mismatch\nwant:\n%s\ngot:\n%s", name, want, got)
	}
}

func providerChannels() []model.Channel {
	return []model.Channel{
		{
			ID:           "beta",
			NormalizedID: "beta",
			Name:         "Apostrophe's \"Channel\"\r\nLive",
			Group:        "Fixture\nGroup",
			OffsetNumber: 1002,
			StreamURL:    "https://upstream.test/beta.m3u8",
			EmittedURL:   "https://proxy.test/stream/fixture/beta.m3u8",
			LogoURL:      "https://logo.test/beta?title=\"live\"",
		},
		{
			ID:           "alpha",
			NormalizedID: "alpha",
			Name:         "Fixture & Channel <One>",
			Group:        "Fixture",
			OffsetNumber: 1001,
			StreamURL:    "https://stream.test/alpha.m3u8",
		},
		{
			ID:           "none",
			NormalizedID: "none",
			Name:         "Unnumbered",
			StreamURL:    "https://stream.test/none.m3u8",
		},
		{
			ID:           "excluded",
			NormalizedID: "excluded",
			Name:         "Excluded",
			OffsetNumber: 1,
			StreamURL:    "https://stream.test/excluded.m3u8",
			Excluded:     true,
		},
	}
}
