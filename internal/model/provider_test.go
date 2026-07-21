package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestProviderJSONRoundTrip(t *testing.T) {
	in := ProviderSettings{
		ID:                  "lg",
		Label:               "LG",
		ChannelNumberOffset: 1000,
		MinChannels:         50,
		RefreshInterval:     6 * time.Hour,
		Exclusions:          []string{"dinospluto-lgus"},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out ProviderSettings
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != "lg" || !out.IsEnabled() || out.RefreshInterval != 6*time.Hour {
		t.Fatalf("%+v", out)
	}
	if len(out.Exclusions) != 1 || out.Exclusions[0] != "dinospluto-lgus" {
		t.Fatalf("exclusions: %v", out.Exclusions)
	}
}

func TestListProvidersSetsID(t *testing.T) {
	list := ListProviders(map[ProviderID]ProviderSettings{
		"b": {Label: "B"},
		"a": {Label: "A"},
	})
	if len(list.Providers) != 2 || list.Providers[0].ID != "a" || list.Providers[1].ID != "b" {
		t.Fatalf("%+v", list)
	}
}

func TestMergeOverlayWinsAndKeepsDefaults(t *testing.T) {
	defaults := ProviderSettings{
		ID:                  "lg",
		Label:               "LG",
		ChannelNumberOffset: 1000,
		MinChannels:         50,
		RefreshInterval:     6 * time.Hour,
		Exclusions:          []string{"default-junk"},
	}
	_ = defaults.CompileExclusions()

	// Empty overlay keeps every default.
	if got := defaults.Merge(ProviderSettings{}); got.MinChannels != 50 || got.ChannelNumberOffset != 1000 || got.Label != "LG" {
		t.Fatalf("empty overlay changed defaults: %+v", got)
	}

	// Set overlay fields win; unset fields keep defaults; ID is never overlaid.
	overlay := ProviderSettings{ID: "ignored", Label: "Custom", MinChannels: 5}
	got := defaults.Merge(overlay)
	if got.ID != "lg" {
		t.Fatalf("ID should not be overlaid: %q", got.ID)
	}
	if got.Label != "Custom" || got.MinChannels != 5 {
		t.Fatalf("overlay did not win: %+v", got)
	}
	if got.ChannelNumberOffset != 1000 || got.RefreshInterval != 6*time.Hour {
		t.Fatalf("unset overlay fields should keep defaults: %+v", got)
	}
	if len(got.Exclusions) != 1 || got.Exclusions[0] != "default-junk" || len(got.ExclusionRegexes) != 1 {
		t.Fatalf("default exclusions should survive empty overlay: %+v", got.Exclusions)
	}
}

func TestMergeExclusionsReplacedWhenOverlaySet(t *testing.T) {
	defaults := ProviderSettings{Exclusions: []string{"default-junk"}}
	_ = defaults.CompileExclusions()

	overlay := ProviderSettings{Exclusions: []string{"user-junk"}}
	if err := overlay.CompileExclusions(); err != nil {
		t.Fatal(err)
	}
	got := defaults.Merge(overlay)
	if len(got.Exclusions) != 1 || got.Exclusions[0] != "user-junk" {
		t.Fatalf("overlay exclusions should replace defaults: %+v", got.Exclusions)
	}
	if len(got.ExclusionRegexes) != 1 || !got.ExclusionRegexes[0].MatchString("USER-JUNK") {
		t.Fatalf("overlay regexes should replace defaults: %+v", got.ExclusionRegexes)
	}
}
