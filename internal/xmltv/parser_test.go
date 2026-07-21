package xmltv_test

import (
	"strings"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/xmltv"
)

func TestParseSanitizesBareAmpersandsAndPreservesEntities(t *testing.T) {
	input := `<?xml version="1.0"?>
<tv>
  <channel id="news&amp;weather"><display-name>News &amp; Weather</display-name></channel>
  <programme channel="news&amp;weather" start="20260721120000 +0000" stop="20260721130000 +0000">
    <title>News & Weather &amp; Traffic &#38; More &nbsp;</title>
    <desc>Storms && forecasts</desc>
  </programme>
</tv>`
	document, err := xmltv.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(document.ChannelIDs) != 1 || document.ChannelIDs[0] != "news&weather" {
		t.Fatalf("channel ids: %+v", document.ChannelIDs)
	}
	if len(document.Programmes) != 1 {
		t.Fatalf("programmes: %+v", document.Programmes)
	}
	programme := document.Programmes[0]
	if programme.ChannelID != "news&weather" || programme.Title != "News & Weather & Traffic & More &nbsp;" {
		t.Fatalf("programme: %+v", programme)
	}
	if programme.Desc != "Storms && forecasts" || !programme.Start.Equal(time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("programme details: %+v", programme)
	}
}

func TestParseSkipsMalformedProgrammeRows(t *testing.T) {
	input := `<tv>
  <channel id="valid"/>
  <programme channel="valid" start="bad" stop="20260721130000 +0000"><title>Bad time</title></programme>
  <programme channel="valid" start="20260721120000 +0000" stop="20260721130000 +0000"><title>Good</title></programme>
</tv>`
	document, err := xmltv.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Programmes) != 1 || document.Programmes[0].Title != "Good" {
		t.Fatalf("programmes: %+v", document.Programmes)
	}
}

func TestParseRejectsMalformedXML(t *testing.T) {
	if _, err := xmltv.Parse(strings.NewReader(`<tv><channel id="broken"></tv>`)); err == nil {
		t.Fatal("malformed XML should fail")
	}
}
