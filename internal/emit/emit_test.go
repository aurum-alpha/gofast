package emit_test

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/emit"
	"github.com/j27-aurum/gofast/internal/model"
)

func TestM3U(t *testing.T) {
	chs := []model.Channel{
		{NormalizedID: "b", Name: `News "One"`, Number: 1002, StreamURL: "https://b", Group: "News", LogoURL: "https://logo/b"},
		{NormalizedID: "a", Name: "Alpha", Number: 1001, StreamURL: "https://a", Group: "News"},
		{NormalizedID: "x", Name: "Excluded", Number: 1, StreamURL: "https://x", Excluded: true},
	}
	var buf bytes.Buffer
	if err := emit.M3U(&buf, chs, "LG"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "#EXTM3U\n") {
		t.Fatalf("header: %q", out)
	}
	if strings.Contains(out, "Excluded") || strings.Contains(out, "https://x") {
		t.Fatal("excluded channel leaked")
	}
	if !strings.Contains(out, `tvg-id="a"`) || !strings.Contains(out, "Alpha · LG") {
		t.Fatalf("missing a: %s", out)
	}
	if !strings.Contains(out, `tvg-name="News One"`) || !strings.Contains(out, `group-title="LG: News"`) {
		t.Fatalf("attrs: %s", out)
	}
	// Sorted by chno: a before b
	if iA, iB := strings.Index(out, "https://a"), strings.Index(out, "https://b"); iA < 0 || iB < 0 || iA > iB {
		t.Fatalf("sort: %s", out)
	}
}

func TestXMLTV(t *testing.T) {
	chs := []model.Channel{
		{NormalizedID: "ch1", Name: "One & Two", Number: 10, StreamURL: "https://s", LogoURL: "https://logo"},
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
	if err := emit.XMLTV(&buf, chs, progs, "LG"); err != nil {
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
