package xmltv_test

import (
	"bytes"
	"encoding/xml"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/xmltv"
)

func TestMarshalProviderGolden(t *testing.T) {
	channels, programmes := providerFixture()
	channelsBefore := append([]model.Channel(nil), channels...)
	programmesBefore := append([]model.Programme(nil), programmes...)
	got, err := xmltv.Marshal(channels, programmes, "TEST_PROVIDER")
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "provider.golden.xml", got)
	if !reflect.DeepEqual(channels, channelsBefore) || !reflect.DeepEqual(programmes, programmesBefore) {
		t.Fatal("marshal mutated caller input")
	}
	assertXMLInvariants(t, got)
}

func TestMarshalAggregateGoldenAndDeterministic(t *testing.T) {
	start := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	sources := []xmltv.Source{
		{
			Provider: "fixture_a",
			Label:    "FIXTURE_A",
			Channels: []model.Channel{
				{ID: "shared", NormalizedID: "shared", Name: "Fixture A Shared", OffsetNumber: 2002, StreamURL: "https://stream.test/fixture-a.m3u8"},
				{ID: "late", NormalizedID: "late", Name: "Fixture A Unnumbered", StreamURL: "https://stream.test/late.m3u8"},
			},
			Programmes: []model.Programme{
				{ChannelID: "late", Title: "Later", Start: start.Add(time.Hour), Stop: start.Add(2 * time.Hour)},
				{ChannelID: "shared", Title: "Fixture A Programme", Start: start, Stop: start.Add(time.Hour)},
			},
		},
		{
			Provider: "fixture_b",
			Label:    "FIXTURE_B",
			Channels: []model.Channel{
				{ID: "shared", NormalizedID: "shared", Name: "Fixture B Shared", OffsetNumber: 1001, StreamURL: "https://stream.test/fixture-b.m3u8"},
			},
			Programmes: []model.Programme{
				{ChannelID: "shared", Title: "Fixture B Programme", Start: start, Stop: start.Add(time.Hour)},
			},
		},
	}

	got, err := xmltv.MarshalAll(sources, xmltv.Options{NamespaceIDs: true})
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "aggregate.golden.xml", got)
	assertXMLInvariants(t, got)

	reversed := []xmltv.Source{sources[1], sources[0]}
	reversed[1].Channels = []model.Channel{sources[0].Channels[1], sources[0].Channels[0]}
	reversed[1].Programmes = []model.Programme{sources[0].Programmes[1], sources[0].Programmes[0]}
	again, err := xmltv.MarshalAll(reversed, xmltv.Options{NamespaceIDs: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(got) {
		t.Fatalf("output changed after input reorder:\n%s\n---\n%s", got, again)
	}
}

func TestMarshalEmitsProgrammeCategories(t *testing.T) {
	start := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	data, err := xmltv.Marshal(
		[]model.Channel{{ID: "a", NormalizedID: "a", Name: "A", StreamURL: "https://x/a"}},
		[]model.Programme{{
			ChannelID:         "a",
			Title:             "Show",
			Start:             start,
			Stop:              start.Add(time.Hour),
			Categories:        []string{"Series"},
			EmittedCategories: []string{"Comedy", "Series"},
		}},
		"Test",
	)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "<category>Comedy</category>") || !strings.Contains(out, "<category>Series</category>") {
		t.Fatalf("missing categories:\n%s", out)
	}
	// EmittedCategories wins over Categories alone.
	if strings.Count(out, "<category>") != 2 {
		t.Fatalf("want 2 category tags:\n%s", out)
	}
}

func TestMarshalRejectsDuplicateIDs(t *testing.T) {
	_, err := xmltv.Marshal([]model.Channel{
		{ID: "a b", NormalizedID: "a_b", Name: "One", StreamURL: "https://stream.test/one.m3u8"},
		{ID: "a_b", NormalizedID: "a_b", Name: "Two", StreamURL: "https://stream.test/two.m3u8"},
	}, nil, "Test")
	if err == nil {
		t.Fatal("expected duplicate emitted id error")
	}
}

func TestMarshalReplacesInvalidXMLCharacter(t *testing.T) {
	data, err := xmltv.Marshal([]model.Channel{{
		ID:           "news",
		NormalizedID: "news",
		Name:         "News\x00",
		StreamURL:    "https://stream.test/news.m3u8",
	}}, nil, "Test")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.IndexByte(data, 0) >= 0 {
		t.Fatalf("invalid XML control leaked: %q", data)
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

func assertXMLInvariants(t *testing.T, data []byte) {
	t.Helper()
	var doc struct {
		XMLName  xml.Name `xml:"tv"`
		Channels []struct {
			ID string `xml:"id,attr"`
		} `xml:"channel"`
		Programmes []struct {
			Channel string `xml:"channel,attr"`
		} `xml:"programme"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	ids := make(map[string]struct{}, len(doc.Channels))
	for _, channel := range doc.Channels {
		if _, duplicate := ids[channel.ID]; duplicate {
			t.Fatalf("duplicate channel id %q", channel.ID)
		}
		ids[channel.ID] = struct{}{}
	}
	for _, programme := range doc.Programmes {
		if _, ok := ids[programme.Channel]; !ok {
			t.Fatalf("orphan programme channel %q", programme.Channel)
		}
	}
}

func providerFixture() ([]model.Channel, []model.Programme) {
	start := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	mountain := time.FixedZone("MDT", -7*60*60)
	channels := []model.Channel{
		{
			ID:           "beta",
			NormalizedID: "beta",
			Name:         "Apostrophe's \"Channel\"",
			OffsetNumber: 1002,
			StreamURL:    "https://stream.test/beta.m3u8",
			LogoURL:      "https://logo.test/beta?a=1&title=\"live\"",
		},
		{
			ID:           "alpha",
			NormalizedID: "alpha",
			Name:         "Fixture & Channel <One>",
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
	programmes := []model.Programme{
		{ChannelID: "beta", Title: "Second", Start: start.Add(2 * time.Hour), Stop: start.Add(3 * time.Hour)},
		{
			ChannelID: "alpha",
			Title:     `Fixture & "Programme"`,
			Desc:      "Apostrophe's <description>",
			Start:     time.Date(2026, 7, 21, 5, 0, 0, 123, mountain),
			Stop:      time.Date(2026, 7, 21, 6, 0, 0, 456, mountain),
		},
		{ChannelID: "beta", Title: "First", Start: start, Stop: start.Add(time.Hour)},
		{ChannelID: "none", Title: "No Number", Start: start, Stop: start.Add(time.Hour)},
		{ChannelID: "excluded", Title: "Hidden", Start: start, Stop: start.Add(time.Hour)},
		{ChannelID: "orphan", Title: "Orphan", Start: start, Stop: start.Add(time.Hour)},
		{ChannelID: "alpha", Title: "Zero", Stop: start.Add(time.Hour)},
		{ChannelID: "alpha", Title: "Reverse", Start: start, Stop: start.Add(-time.Hour)},
		{ChannelID: "alpha", Title: " ", Start: start, Stop: start.Add(time.Hour)},
	}
	return channels, programmes
}
