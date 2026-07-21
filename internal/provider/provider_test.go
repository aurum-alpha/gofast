package provider

import (
	"testing"

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
}

func TestRegistryChannelsMergeAndSort(t *testing.T) {
	settings := map[model.ProviderID]model.ProviderSettings{"lg": {ID: "lg"}}
	reg := NewRegistry(map[model.ProviderID]Reader{"lg": fakeReader{}}, settings)
	f, _ := reg.Feed("lg")
	f.Set(Lineup{Channels: []model.Channel{
		{Provider: "lg", NormalizedID: "b", OffsetNumber: 2},
		{Provider: "lg", NormalizedID: "a", OffsetNumber: 1},
	}})

	chs := reg.Channels()
	if len(chs) != 2 || chs[0].NormalizedID != "a" || chs[1].NormalizedID != "b" {
		t.Fatalf("merged+sorted channels: %+v", chs)
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
