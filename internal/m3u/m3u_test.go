package m3u_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/j27-aurum/gofast/internal/m3u"
	"github.com/j27-aurum/gofast/internal/model"
)

func TestWrite(t *testing.T) {
	chs := []model.Channel{
		{NormalizedID: "b", Name: `News "One"`, Number: 2, OffsetNumber: 1002, StreamURL: "https://b", Group: "News", LogoURL: "https://logo/b"},
		{NormalizedID: "a", Name: "Alpha", Number: 1, OffsetNumber: 1001, StreamURL: "https://a", Group: "News"},
		{NormalizedID: "x", Name: "Excluded", Number: 1, OffsetNumber: 1, StreamURL: "https://x", Excluded: true},
	}
	var buf bytes.Buffer
	if err := m3u.Write(&buf, chs, "LG"); err != nil {
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
	// Sorted by channel number: a before b
	if iA, iB := strings.Index(out, "https://a"), strings.Index(out, "https://b"); iA < 0 || iB < 0 || iA > iB {
		t.Fatalf("sort: %s", out)
	}
}

func TestWriteAllNamespacesIDs(t *testing.T) {
	sources := []m3u.Source{
		{Provider: "lg", Label: "LG", Channels: []model.Channel{
			{NormalizedID: "news", Name: "News", OffsetNumber: 1005, StreamURL: "https://s"},
		}},
	}

	var bare bytes.Buffer
	if err := m3u.WriteAll(&bare, sources, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(bare.String(), `tvg-id="news"`) || strings.Contains(bare.String(), "lg.news") {
		t.Fatalf("bare: %s", bare.String())
	}

	var ns bytes.Buffer
	if err := m3u.WriteAll(&ns, sources, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ns.String(), `tvg-id="lg.news"`) {
		t.Fatalf("namespaced: %s", ns.String())
	}
}
