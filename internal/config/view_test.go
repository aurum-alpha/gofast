package config

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"
)

func TestViewProvidersSorted(t *testing.T) {
	enabled := true
	cfg := Config{
		Listen:  ":8180",
		BaseURL: "http://x:8180",
		DataDir: "/data",
		Providers: map[string]Provider{
			"pluto": {Label: "Pluto", Enabled: &enabled, Region: "us"},
			"lg":    {Label: "LG", Exclusions: []string{"a", "b"}},
		},
	}
	view := ViewProviders("/data/config.yaml", true, cfg)
	if !view.FromFile || view.Path != "/data/config.yaml" {
		t.Fatalf("meta: %+v", view)
	}
	if len(view.Providers) != 2 || view.Providers[0].ID != "lg" || view.Providers[1].ID != "pluto" {
		t.Fatalf("order: %+v", view.Providers)
	}
	if view.Providers[0].Exclusions != 2 || !view.Providers[0].Enabled {
		t.Fatalf("lg: %+v", view.Providers[0])
	}
	b, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(b) {
		t.Fatal("invalid json")
	}
}

func TestViewProvidersFromExample(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "config.example.yaml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	view := ViewProviders(path, true, cfg)
	if len(view.Providers) != 7 {
		t.Fatalf("got %d", len(view.Providers))
	}
}
