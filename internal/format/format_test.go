package format

import "testing"

func TestStripQuotes(t *testing.T) {
	if got := StripQuotes(`Foo "Bar" Baz`); got != "Foo Bar Baz" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatDisplayNameAndGroupTitle(t *testing.T) {
	if got := FormatDisplayName("CNN", "LG"); got != "CNN · LG" {
		t.Fatalf("display: %q", got)
	}
	if got := FormatDisplayName("CNN", ""); got != "CNN" {
		t.Fatalf("display no label: %q", got)
	}
	if got := FormatGroupTitle("Pluto", "News"); got != "Pluto: News" {
		t.Fatalf("group: %q", got)
	}
	if got := FormatGroupTitle("Pluto", ""); got != "Pluto" {
		t.Fatalf("group empty: %q", got)
	}
}
