package provider

import (
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
)

func TestRegistryOnlyWiredReadersAreEnabled(t *testing.T) {
	// settings covers all known ids; readers holds only the enabled ones.
	settings := map[model.ProviderID]model.ProviderSettings{
		"lg":    {ID: "lg", Label: "LG"},
		"pluto": {ID: "pluto", Label: "Pluto"},
	}
	reg := NewRegistry(map[model.ProviderID]Reader{"lg": fakeReader{}}, settings)

	ids := reg.IDs()
	if len(ids) != 1 || ids[0] != "lg" {
		t.Fatalf("enabled ids: %v", ids)
	}
	// Disabled/unwired provider still has settings and appears in the API list.
	if reg.Settings("pluto").Label != "Pluto" {
		t.Fatalf("settings should be retained: %+v", reg.Settings("pluto"))
	}
	if len(reg.Providers().Providers) != 2 {
		t.Fatalf("all known providers should be listed: %+v", reg.Providers())
	}
	if _, ok := reg.Feed("pluto"); ok {
		t.Fatal("disabled provider should have no feed")
	}
	if got, ok := reg.Provider("pluto"); !ok || got.Label != "Pluto" {
		t.Fatalf("Provider lookup: %+v ok=%v", got, ok)
	}
}

func TestFeedStats(t *testing.T) {
	reg := NewRegistry(
		map[model.ProviderID]Reader{"lg": fakeReader{}},
		map[model.ProviderID]model.ProviderSettings{"lg": {ID: "lg"}},
	)
	feed, _ := reg.Feed("lg")
	start := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	feed.Set(Lineup{
		Channels: []model.Channel{
			{NormalizedID: "news", StreamURL: "https://news", Group: "News", Region: "US", Classification: model.ClassNative},
			{NormalizedID: "drm", StreamURL: "https://drm", Group: "Movies", Region: "ca", Classification: model.ClassDRM, Excluded: true, FilterReason: "DRM"},
			{NormalizedID: "other", StreamURL: "https://other"},
		},
		Programmes: []model.Programme{
			{ChannelID: "news", Title: "News", Start: start, Stop: start.Add(time.Hour)},
			{ChannelID: "drm", Title: "Movie", Start: start.Add(-time.Hour), Stop: start.Add(2 * time.Hour)},
			{ChannelID: "other", Title: "", Start: start, Stop: start.Add(time.Hour)},
		},
		ChannelCount:   2,
		ProgrammeCount: 1,
		FetchedAt:      start,
	})
	feed.SetStatus(Status{LastAttemptAt: start.Add(time.Hour), LastError: "failed", LastErrorAt: start.Add(time.Hour)})

	stats := feed.Stats()
	if stats.TotalChannels != 3 || stats.ExportedChannels != 2 || stats.ExcludedChannels != 1 {
		t.Fatalf("channel stats: %+v", stats)
	}
	if stats.TotalProgrammes != 3 || stats.ExportedProgrammes != 1 {
		t.Fatalf("programme stats: %+v", stats)
	}
	if stats.ByClassification["NATIVE"] != 1 || stats.ByClassification["DRM"] != 1 || stats.ByClassification["UNCLASSIFIED"] != 1 {
		t.Fatalf("classifications: %+v", stats.ByClassification)
	}
	if stats.ByGroup["(none)"] != 1 || stats.FilterReasons["DRM"] != 1 {
		t.Fatalf("rollups: groups=%+v reasons=%+v", stats.ByGroup, stats.FilterReasons)
	}
	if stats.ByRegion["US"] != 1 || stats.ByRegion["CA"] != 1 || stats.ByRegion["(none)"] != 1 {
		t.Fatalf("regions: %+v", stats.ByRegion)
	}
	if !stats.GuideStart.Equal(start) || !stats.GuideEnd.Equal(start.Add(time.Hour)) || stats.LastError != "failed" {
		t.Fatalf("times/status: %+v", stats)
	}
}

func TestRegistryChannelsMergeAndSort(t *testing.T) {
	settings := map[model.ProviderID]model.ProviderSettings{
		"lg":    {ID: "lg"},
		"pluto": {ID: "pluto"},
	}
	reg := NewRegistry(map[model.ProviderID]Reader{
		"lg":    fakeReader{},
		"pluto": fakeReader{},
	}, settings)
	lg, _ := reg.Feed("lg")
	lg.Set(Lineup{Channels: []model.Channel{
		{Provider: "lg", NormalizedID: "b", OffsetNumber: 2002},
		{Provider: "lg", NormalizedID: "a", OffsetNumber: 1001},
	}})
	pluto, _ := reg.Feed("pluto")
	pluto.Set(Lineup{Channels: []model.Channel{
		{Provider: "pluto", NormalizedID: "p", OffsetNumber: 1500},
		{Provider: "pluto", NormalizedID: "z", OffsetNumber: 0},
	}})

	chs := reg.Channels()
	want := []struct {
		provider string
		id       string
		num      int
	}{
		{"lg", "a", 1001},
		{"pluto", "p", 1500},
		{"lg", "b", 2002},
		{"pluto", "z", 0},
	}
	if len(chs) != len(want) {
		t.Fatalf("len=%d want %d: %+v", len(chs), len(want), chs)
	}
	for i, w := range want {
		if string(chs[i].Provider) != w.provider || chs[i].NormalizedID != w.id || chs[i].OffsetNumber != w.num {
			t.Fatalf("chs[%d]=%+v want provider=%s id=%s num=%d", i, chs[i], w.provider, w.id, w.num)
		}
	}
}

func TestFeedAccessorsReturnCopies(t *testing.T) {
	settings := map[model.ProviderID]model.ProviderSettings{"lg": {ID: "lg", Label: "LG"}}
	reg := NewRegistry(map[model.ProviderID]Reader{"lg": fakeReader{}}, settings)
	f, _ := reg.Feed("lg")
	f.Set(Lineup{Channels: []model.Channel{{NormalizedID: "a"}}})

	got := f.Channels()
	got[0].NormalizedID = "mutated"
	if f.Channels()[0].NormalizedID != "a" {
		t.Fatal("Channels() must return a copy")
	}
	if f.ID() != "lg" || f.Label() != "LG" {
		t.Fatalf("meta: id=%q label=%q", f.ID(), f.Label())
	}
}

func TestSkipMalformed(t *testing.T) {
	n := 0
	SkipMalformed(&n)
	SkipMalformed(&n)
	if n != 2 {
		t.Fatalf("got %d", n)
	}
	SkipMalformed(nil) // must not panic
}
