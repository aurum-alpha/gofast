package model

import (
	"regexp"
	"testing"
)

func TestNormalizeIDDistroTVSpaces(t *testing.T) {
	got := NormalizeID("dtv_EPGACE TV")
	want := "dtv_EPGACE_TV"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestNormalizeIDStableAndHostile(t *testing.T) {
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
		if got := NormalizeID(tt.in); got != tt.want {
			t.Errorf("NormalizeID(%q)=%q want %q", tt.in, got, tt.want)
		}
		if NormalizeID(tt.in) != NormalizeID(tt.in) {
			t.Errorf("unstable for %q", tt.in)
		}
	}
}

func TestApplyChannelNumberOffset(t *testing.T) {
	ch := Channel{Number: 42}
	ch.ApplyChannelNumberOffset(1000)
	if ch.Number != 42 || ch.OffsetNumber != 1042 {
		t.Fatalf("number=%d offset_number=%d", ch.Number, ch.OffsetNumber)
	}
	ch = Channel{Number: 0}
	ch.ApplyChannelNumberOffset(1000)
	if ch.Number != 0 || ch.OffsetNumber != 0 {
		t.Fatalf("no native should stay 0, number=%d offset_number=%d", ch.Number, ch.OffsetNumber)
	}
	ch = Channel{Number: -1}
	ch.ApplyChannelNumberOffset(1000)
	if ch.Number != 0 || ch.OffsetNumber != 0 {
		t.Fatalf("negative native: number=%d offset_number=%d", ch.Number, ch.OffsetNumber)
	}
}

func TestChannelNormalize(t *testing.T) {
	ch := Channel{
		ID:    "dtv_EPGACE TV",
		Name:  `A&E "Crime"`,
		Group: "News",
	}
	ch.Normalize()
	if ch.NormalizedID != "dtv_EPGACE_TV" {
		t.Fatalf("id: %q", ch.NormalizedID)
	}
	if ch.Name != "A&E Crime" {
		t.Fatalf("name: %q", ch.Name)
	}
}

func TestDinosplutoExclusion(t *testing.T) {
	re := regexp.MustCompile("(?i)dinospluto-lgus")
	ch := Channel{
		Provider:  "lg",
		ID:        "something",
		Name:      "Junk",
		StreamURL: "https://cdn.example/dinospluto-lgus/master.m3u8",
	}
	ch.Normalize()
	ok, reason := ch.MatchesExclusion([]*regexp.Regexp{re})
	if !ok {
		t.Fatal("expected exclusion match")
	}
	if reason == "" {
		t.Fatal("expected reason")
	}

	clean := Channel{
		Provider:  "lg",
		ID:        "ok",
		Name:      "Real",
		StreamURL: "https://cdn.example/real/master.m3u8",
	}
	if ok, _ := clean.MatchesExclusion([]*regexp.Regexp{re}); ok {
		t.Fatal("clean channel should not match")
	}
}

func TestForExport(t *testing.T) {
	chs := []Channel{
		{NormalizedID: "a", StreamURL: "https://a"},
		{NormalizedID: "b", StreamURL: "https://b", Excluded: true},
		{NormalizedID: "", StreamURL: "https://c"},
		{NormalizedID: "d", StreamURL: ""},
	}
	out := ForExport(chs)
	if len(out) != 1 || out[0].NormalizedID != "a" {
		t.Fatalf("%+v", out)
	}
}

func TestMarkExclusions(t *testing.T) {
	re := regexp.MustCompile("(?i)blocked")
	chs := []Channel{
		{Name: "Good", StreamURL: "https://ok"},
		{Name: "Bad", StreamURL: "https://blocked.example"},
	}
	out := MarkExclusions(chs, []*regexp.Regexp{re})
	if out[0].Excluded || out[1].Excluded == false {
		t.Fatalf("%+v", out)
	}
	if out[1].FilterReason == "" {
		t.Fatal("missing reason")
	}
}
