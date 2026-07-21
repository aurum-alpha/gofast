package format

import "testing"

func TestFormatDisplayNameAndGroupTitlePreservePunctuation(t *testing.T) {
	if got := FormatDisplayName(`Bob's "News"`, "LG"); got != `Bob's "News" · LG` {
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

func TestM3USanitation(t *testing.T) {
	input := "  Bob's \"News\"\r\nLive\x00  "
	if got := M3UAttribute(input); got != "Bob's News Live" {
		t.Fatalf("attribute: %q", got)
	}
	if got := M3UText(input); got != "Bob's \"News\" Live" {
		t.Fatalf("text: %q", got)
	}
}

func TestValidM3ULine(t *testing.T) {
	if !ValidM3ULine("https://example.test/live.m3u8?token=a'b") {
		t.Fatal("valid URL rejected")
	}
	for _, invalid := range []string{
		"",
		"https://example.test/live.m3u8\n#EXTINF:-1",
		"https://example.test/live.m3u8\x00",
		string([]byte{0xff}),
	} {
		if ValidM3ULine(invalid) {
			t.Fatalf("invalid line accepted: %q", invalid)
		}
	}
}
