package refresh

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/provider/lg"
)

func TestProviderRefresherPublishesAndArchives(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "provider", "lg", "testdata", "schedulelist.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	cc := cache.New(dir)
	settings := lg.DefaultSettings().Merge(model.ProviderSettings{MinChannels: 1, ChannelsURL: srv.URL})
	reader := lg.New(settings, httpx.NewClient(5*time.Second, 0), cc)
	reg := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{model.ProviderLG: reader},
		map[model.ProviderID]model.ProviderSettings{model.ProviderLG: settings},
	)
	f, _ := reg.Feed(model.ProviderLG)

	notified := 0
	pr := &providerRefresher{feed: f, cache: cc, notify: func() { notified++ }}
	if err := pr.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(f.Channels()) == 0 || f.FetchedAt().IsZero() {
		t.Fatalf("feed not populated: %+v", f.Lineup())
	}
	if _, err := cc.ReadM3U(model.ProviderLG); err != nil {
		t.Fatalf("m3u not written: %v", err)
	}
	if _, err := cc.ReadXMLTV(model.ProviderLG); err != nil {
		t.Fatalf("xml not written: %v", err)
	}
	if notified != 1 {
		t.Fatalf("notify called %d times, want 1", notified)
	}
	if _, err := os.Stat(filepath.Join(dir, "lg", "raw")); err != nil {
		t.Fatalf("raw not archived: %v", err)
	}
}

func TestRestoreRehydratesFromRaw(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "provider", "lg", "testdata", "schedulelist.json"))
	if err != nil {
		t.Fatal(err)
	}
	cc := cache.New(t.TempDir())

	// Persist only the raw upstream snapshot + a slim meta (fetch time + one
	// classification). Channels/programmes are NOT persisted — Restore re-parses raw.
	if err := cc.WriteRaw(model.ProviderLG, fixture); err != nil {
		t.Fatal(err)
	}
	fetchedAt := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	meta := provider.Meta{
		FetchedAt:       fetchedAt,
		Classifications: map[string]model.Classification{"ch-news": model.ClassBeacon},
	}
	if err := cc.WriteProvider(model.ProviderLG, nil, nil, meta); err != nil {
		t.Fatal(err)
	}

	settings := lg.DefaultSettings().Merge(model.ProviderSettings{MinChannels: 1})
	reg := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{model.ProviderLG: lg.New(settings, nil, cc)},
		map[model.ProviderID]model.ProviderSettings{model.ProviderLG: settings},
	)

	Restore(reg, cc)

	f, _ := reg.Feed(model.ProviderLG)
	if len(f.Channels()) == 0 {
		t.Fatalf("feed not rehydrated from raw: %+v", f.Lineup())
	}
	if !f.FetchedAt().Equal(fetchedAt) {
		t.Fatalf("fetched_at not restored: got %v want %v", f.FetchedAt(), fetchedAt)
	}
	// Persisted classification was applied (not recomputed).
	var found bool
	for _, ch := range f.Channels() {
		if ch.NormalizedID == "ch-news" {
			found = true
			if ch.Classification != model.ClassBeacon {
				t.Fatalf("classification not applied: %+v", ch)
			}
		}
	}
	if !found {
		t.Fatal("expected ch-news in rehydrated lineup")
	}
}
