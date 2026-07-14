package provider

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/j27-aurum/gofast/internal/model"
)

func TestRegistryOnlyWiredReadersAreEnabled(t *testing.T) {
	// settings covers all known ids (enabled + disabled); readers holds only the
	// enabled ones — enable/disable is decided in the bootstrap, not the registry.
	settings := map[string]model.ProviderSettings{
		"lg":    {Label: "LG"},
		"pluto": {Label: "Pluto"},
	}
	readers := map[string]Reader{
		"lg": &fakeReader{id: "lg", Channels: []model.Channel{{ID: "1", Name: "One", StreamURL: "https://x"}}},
	}
	reg := NewRegistry(readers, settings)
	ids := reg.IDs()
	if len(ids) != 1 || ids[0] != "lg" {
		t.Fatalf("ids: %v", ids)
	}
}

func TestRegistryOmitsProvidersWithoutReader(t *testing.T) {
	settings := map[string]model.ProviderSettings{"lg": {Label: "LG"}}
	reg := NewRegistry(map[string]Reader{}, settings)
	if len(reg.IDs()) != 0 {
		t.Fatalf("expected no enabled readers, got %v", reg.IDs())
	}
	// Settings are still available for the API/logs even without a reader.
	if reg.Settings("lg").Label != "LG" {
		t.Fatalf("settings should be retained: %+v", reg.Settings("lg"))
	}
}

func TestFakeRoundTripFetchAll(t *testing.T) {
	re := regexp.MustCompile("(?i)dinospluto-lgus")
	settings := map[string]model.ProviderSettings{
		"lg": {
			Label:               "LG",
			ChannelNumberOffset: 1000,
			Exclusions:          []string{"dinospluto-lgus"},
			ExclusionRegexes:    []*regexp.Regexp{re},
		},
	}
	fake := &fakeReader{
		id: "lg",
		Channels: []model.Channel{
			{ID: "dtv_EPGACE TV", Name: "Good", Number: 5, StreamURL: "https://ok/stream"},
			{ID: "junk", Name: "Bad", StreamURL: "https://cdn/dinospluto-lgus/x"},
		},
		Programmes: []model.Programme{
			{ChannelID: "dtv_EPGACE_TV", Title: "Show"},
		},
	}
	reg := NewRegistry(map[string]Reader{"lg": fake}, settings)
	results := reg.FetchAll(context.Background())
	if len(results) != 1 {
		t.Fatalf("results: %d", len(results))
	}
	res := results[0]
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if len(res.Channels) != 2 {
		t.Fatalf("channels: %d", len(res.Channels))
	}
	good, bad := res.Channels[0], res.Channels[1]
	if good.NormalizedID != "dtv_EPGACE_TV" || good.Number != 5 || good.OffsetNumber != 1005 || good.Excluded {
		t.Fatalf("good: %+v", good)
	}
	if !bad.Excluded {
		t.Fatalf("bad should be excluded: %+v", bad)
	}
	if len(res.Programmes) != 1 {
		t.Fatalf("programmes: %d", len(res.Programmes))
	}
}

func TestFetchAllRecordsProviderError(t *testing.T) {
	settings := map[string]model.ProviderSettings{"lg": {Label: "LG"}}
	readers := map[string]Reader{
		"lg": &fakeReader{id: "lg", Err: errors.New("upstream down")},
	}
	reg := NewRegistry(readers, settings)
	res := reg.FetchAll(context.Background())
	if len(res) != 1 || res[0].Err == nil {
		t.Fatalf("%+v", res)
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
