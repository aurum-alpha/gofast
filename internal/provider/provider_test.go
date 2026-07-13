package provider

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/model"
)

func boolPtr(v bool) *bool { return &v }

func TestRegistryEnableDisable(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]model.Provider{
			"lg": {
				Enabled: boolPtr(true),
				Label:   "LG",
			},
			"pluto": {
				Enabled: boolPtr(false),
				Label:   "Pluto",
			},
		},
	}
	fake := &fakeReader{
		Channels: []model.Channel{{ID: "1", Name: "One", StreamURL: "https://x"}},
	}
	reg, err := NewRegistry(cfg, map[string]Factory{
		"lg":    fakeFactory(fake),
		"pluto": fakeFactory(fake),
	})
	if err != nil {
		t.Fatal(err)
	}
	ids := reg.IDs()
	if len(ids) != 1 || ids[0] != "lg" {
		t.Fatalf("ids: %v", ids)
	}
}

func TestRegistrySkipsUnknownFactory(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]model.Provider{
			"lg": {Label: "LG"},
		},
	}
	reg, err := NewRegistry(cfg, map[string]Factory{})
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.IDs()) != 0 {
		t.Fatalf("expected empty registry, got %v", reg.IDs())
	}
}

func TestFakeRoundTripFetchAll(t *testing.T) {
	re := regexp.MustCompile("(?i)dinospluto-lgus")
	cfg := &config.Config{
		Providers: map[string]model.Provider{
			"lg": {
				Label:            "LG",
				ChnoOffset:       1000,
				Exclusions:       []string{"dinospluto-lgus"},
				ExclusionRegexes: []*regexp.Regexp{re},
			},
		},
	}
	fake := &fakeReader{
		Channels: []model.Channel{
			{ID: "dtv_EPGACE TV", Name: "Good", Number: 5, StreamURL: "https://ok/stream"},
			{ID: "junk", Name: "Bad", StreamURL: "https://cdn/dinospluto-lgus/x"},
		},
		Programmes: []model.Programme{
			{ChannelID: "dtv_EPGACE_TV", Title: "Show"},
		},
	}
	reg, err := NewRegistry(cfg, map[string]Factory{"lg": fakeFactory(fake)})
	if err != nil {
		t.Fatal(err)
	}
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
	if good.NormalizedID != "dtv_EPGACE_TV" || good.Number != 1005 || good.Excluded {
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
	cfg := &config.Config{
		Providers: map[string]model.Provider{
			"lg": {Label: "LG"},
		},
	}
	reg, err := NewRegistry(cfg, map[string]Factory{
		"lg": fakeFactory(&fakeReader{Err: errors.New("upstream down")}),
	})
	if err != nil {
		t.Fatal(err)
	}
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
