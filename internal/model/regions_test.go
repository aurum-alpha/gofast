package model

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParseRegionList(t *testing.T) {
	got := ParseRegionList(" us, CA, us ,qq,, ")
	want := []string{"US", "CA", "QQ"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if len(ParseRegionList("")) != 0 {
		t.Fatal("empty should yield nil/empty")
	}
}

func TestNormalizeRegionsCSV(t *testing.T) {
	if got := NormalizeRegionsCSV(""); got != DefaultRegions {
		t.Fatalf("empty → %q", got)
	}
	if got := NormalizeRegionsCSV("ca, us"); got != "CA,US" {
		t.Fatalf("got %q", got)
	}
}

func TestUnmarshalRegionsYAML(t *testing.T) {
	var doc struct {
		Regions yaml.Node `yaml:"regions"`
	}
	if err := yaml.Unmarshal([]byte("regions: [us, ca]\n"), &doc); err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalRegionsYAML(&doc.Regions)
	if err != nil {
		t.Fatal(err)
	}
	if got != "US,CA" {
		t.Fatalf("sequence: %q", got)
	}
	if err := yaml.Unmarshal([]byte("regions: \"us,QQ\"\n"), &doc); err != nil {
		t.Fatal(err)
	}
	got, err = UnmarshalRegionsYAML(&doc.Regions)
	if err != nil {
		t.Fatal(err)
	}
	if got != "US,QQ" {
		t.Fatalf("scalar: %q", got)
	}
}

func TestNormalizeRegionCode(t *testing.T) {
	if got := NormalizeRegionCode(" us "); got != "US" {
		t.Fatalf("got %q", got)
	}
}
