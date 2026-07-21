package m3u_test

import (
	"strings"
	"testing"

	"github.com/j27-aurum/gofast/internal/m3u"
)

func TestParseEXTINFAndNormalizedDeduplication(t *testing.T) {
	input := "\uFEFF#EXTM3U\r\n" +
		`#EXTINF:-1 group-title="Entertainment" tvg-logo="https://logo" tvg-name="Ace, Live" tvg-id="dtv_EPGACE TV" tvg-chno="12",Fallback Name` + "\r\n" +
		"# comment between metadata and URL\r\n" +
		"https://example.test/ace.m3u8\r\n" +
		`#EXTINF:-1 tvg-id="dtv_EPGACE_TV" tvg-name="Duplicate",Duplicate` + "\n" +
		"https://example.test/duplicate.m3u8\n"

	channels, err := m3u.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 1 {
		t.Fatalf("channels: %+v", channels)
	}
	channel := channels[0]
	if channel.ID != "dtv_EPGACE TV" || channel.Name != "Ace, Live" || channel.Number != 12 {
		t.Fatalf("channel: %+v", channel)
	}
	if channel.Group != "Entertainment" || channel.LogoURL != "https://logo" || channel.StreamURL != "https://example.test/ace.m3u8" {
		t.Fatalf("channel metadata: %+v", channel)
	}
}

func TestParseLongURLAndMalformedRows(t *testing.T) {
	longURL := "https://example.test/live.m3u8?" + strings.Repeat("a", 128<<10)
	input := "#EXTM3U\n" +
		"#EXTINF:-1 tvg-name=\"Missing ID\",Missing ID\nhttps://example.test/missing.m3u8\n" +
		"#EXTINF malformed\n" +
		"#EXTINF:-1 tvg-id=\"valid\",Display fallback\n" + longURL + "\n"

	channels, err := m3u.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 1 || channels[0].Name != "Display fallback" || channels[0].StreamURL != longURL {
		t.Fatalf("channels: %+v", channels)
	}
}
