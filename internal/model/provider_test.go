package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestProviderJSONRoundTrip(t *testing.T) {
	in := Provider{
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
	var out Provider
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
	list := ListProviders(map[string]Provider{
		"b": {Label: "B"},
		"a": {Label: "A"},
	})
	if len(list.Providers) != 2 || list.Providers[0].ID != "a" || list.Providers[1].ID != "b" {
		t.Fatalf("%+v", list)
	}
}
