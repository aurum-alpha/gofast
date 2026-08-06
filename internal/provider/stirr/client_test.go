package stirr

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

func TestParseFixture(t *testing.T) {
	list, err := os.ReadFile(filepath.Join("testdata", "list.json"))
	if err != nil {
		t.Fatal(err)
	}
	epg, err := os.ReadFile(filepath.Join("testdata", "epg.json"))
	if err != nil {
		t.Fatal(err)
	}
	c := New(DefaultSettings(), nil)
	chs, progs, err := c.Parse(provider.Raw{
		RawList: list,
		RawEPG:  epg,
		RawDead: []byte(`["6485"]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(chs) != 3 {
		t.Fatalf("channels=%d want 3", len(chs))
	}
	byID := map[string]model.Channel{}
	for _, ch := range chs {
		byID[ch.ID] = ch
		if ch.Classification != model.ClassStirrResolve {
			t.Fatalf("%s class=%s", ch.ID, ch.Classification)
		}
		if ch.StreamURL != OpaqueStreamURL(ch.ID) {
			t.Fatalf("%s stream=%s", ch.ID, ch.StreamURL)
		}
		if ch.RequestHeaders["Origin"] != "https://stirr.com" {
			t.Fatalf("headers: %+v", ch.RequestHeaders)
		}
	}
	drone := byID["5407"]
	if drone.Name != "Drone TV" || drone.Group != "Sports" {
		t.Fatalf("drone: %+v", drone)
	}
	if drone.LogoURL == "" {
		t.Fatal("expected logo")
	}
	komo := byID["6485"]
	if !komo.Excluded || !model.HasFilterReason(komo.FilterReasons, model.FilterReasonDeadSSAI) {
		t.Fatalf("expected dead SSAI filter on 6485: %+v", komo)
	}
	if byID["5407"].Excluded {
		t.Fatal("5407 should not be dead-filtered")
	}
	if len(progs) == 0 {
		t.Fatal("expected programmes from bulk epg")
	}
	var gagsProgs int
	for _, p := range progs {
		if p.ChannelID == "5735" {
			gagsProgs++
		}
	}
	if gagsProgs == 0 {
		t.Fatal("expected programmes for 5735")
	}
}

func TestParseCatalogRows(t *testing.T) {
	list, err := os.ReadFile(filepath.Join("testdata", "list.json"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ParseCatalogRows(list)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows=%d", len(rows))
	}
}
