package xmltv_test

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/xmltv"
)

func TestWrite(t *testing.T) {
	chs := []model.Channel{
		{NormalizedID: "ch1", Name: "One & Two", Number: 10, OffsetNumber: 10, StreamURL: "https://s", LogoURL: "https://logo"},
		{NormalizedID: "drop", Name: "Bad", StreamURL: "https://x", Excluded: true},
	}
	progs := []model.Programme{
		{
			ChannelID: "ch1",
			Title:     "Morning & News",
			Desc:      "Desc",
			Start:     time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC),
			Stop:      time.Date(2024, 6, 1, 13, 0, 0, 0, time.UTC),
		},
		{ChannelID: "drop", Title: "Nope", Start: time.Now(), Stop: time.Now().Add(time.Hour)},
	}
	var buf bytes.Buffer
	if err := xmltv.Write(&buf, chs, progs, "LG"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "One &amp; Two") && !strings.Contains(out, "One & Two") {
		// encoding/xml escapes &
		if !strings.Contains(out, "&amp;") {
			t.Fatalf("expected escaped ampersand: %s", out)
		}
	}
	if strings.Contains(out, `channel="drop"`) || strings.Contains(out, `id="drop"`) {
		t.Fatal("excluded leaked")
	}
	if !strings.Contains(out, `start="20240601120000 +0000"`) {
		t.Fatalf("time: %s", out)
	}
	var doc struct {
		XMLName xml.Name `xml:"tv"`
	}
	if err := xml.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
}
