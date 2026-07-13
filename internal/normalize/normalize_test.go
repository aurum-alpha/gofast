package normalize

import (
	"regexp"
	"testing"

	"github.com/j27-aurum/gofast/internal/model"
)

func TestIDDistroTVSpaces(t *testing.T) {
	got := ID("dtv_EPGACE TV")
	want := "dtv_EPGACE_TV"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestIDStableAndHostile(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"A&E Crime 360", "AE_Crime_360"},
		{`Name "quoted"`, "Name_quoted"},
		{"foo<script>", "fooscript"},
		{"  a  b  ", "a_b"},
		{"ok.id-1", "ok.id-1"},
		{"pluto/us/123", "plutous123"},
	}
	for _, tt := range tests {
		if got := ID(tt.in); got != tt.want {
			t.Errorf("ID(%q)=%q want %q", tt.in, got, tt.want)
		}
		if ID(tt.in) != ID(tt.in) {
			t.Errorf("unstable for %q", tt.in)
		}
	}
}

func TestStripQuotes(t *testing.T) {
	if got := StripQuotes(`Foo "Bar" Baz`); got != "Foo Bar Baz" {
		t.Fatalf("got %q", got)
	}
}

func TestDisplayNameAndGroupTitle(t *testing.T) {
	if got := DisplayName("CNN", "LG"); got != "CNN · LG" {
		t.Fatalf("display: %q", got)
	}
	if got := DisplayName("CNN", ""); got != "CNN" {
		t.Fatalf("display no label: %q", got)
	}
	if got := GroupTitle("Pluto", "News"); got != "Pluto: News" {
		t.Fatalf("group: %q", got)
	}
	if got := GroupTitle("Pluto", ""); got != "Pluto" {
		t.Fatalf("group empty: %q", got)
	}
}

func TestApplyChnoOffset(t *testing.T) {
	if got := ApplyChnoOffset(42, 1000); got != 1042 {
		t.Fatalf("got %d", got)
	}
	if got := ApplyChnoOffset(0, 1000); got != 0 {
		t.Fatalf("no native should stay 0, got %d", got)
	}
	if got := ApplyChnoOffset(-1, 1000); got != 0 {
		t.Fatalf("negative native: %d", got)
	}
}

func TestApplyChannel(t *testing.T) {
	ch := model.Channel{
		ID:    "dtv_EPGACE TV",
		Name:  `A&E "Crime"`,
		Group: "News",
	}
	ApplyChannel(&ch)
	if ch.NormalizedID != "dtv_EPGACE_TV" {
		t.Fatalf("id: %q", ch.NormalizedID)
	}
	if ch.Name != "A&E Crime" {
		t.Fatalf("name: %q", ch.Name)
	}
}

func TestDinosplutoExclusion(t *testing.T) {
	re := regexp.MustCompile("(?i)dinospluto-lgus")
	ch := model.Channel{
		Provider:  "lg",
		ID:        "something",
		Name:      "Junk",
		StreamURL: "https://cdn.example/dinospluto-lgus/master.m3u8",
	}
	ApplyChannel(&ch)
	ok, reason := MatchExclusion(ch, []*regexp.Regexp{re})
	if !ok {
		t.Fatal("expected exclusion match")
	}
	if reason == "" {
		t.Fatal("expected reason")
	}

	clean := model.Channel{
		Provider:  "lg",
		ID:        "ok",
		Name:      "Real",
		StreamURL: "https://cdn.example/real/master.m3u8",
	}
	if ok, _ := MatchExclusion(clean, []*regexp.Regexp{re}); ok {
		t.Fatal("clean channel should not match")
	}
}

func TestApplyExclusionsMarksChannel(t *testing.T) {
	re := regexp.MustCompile("(?i)blocked")
	chs := []model.Channel{
		{Name: "Good", StreamURL: "https://ok"},
		{Name: "Bad", StreamURL: "https://blocked.example"},
	}
	out := ApplyExclusions(chs, []*regexp.Regexp{re})
	if out[0].Excluded || out[1].Excluded == false {
		t.Fatalf("%+v", out)
	}
	if out[1].FilterReason == "" {
		t.Fatal("missing reason")
	}
}
