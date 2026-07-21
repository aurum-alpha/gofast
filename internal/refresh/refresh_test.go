package refresh

import (
	"context"
	"errors"
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

type failingReader struct {
	err error
}

func (r failingReader) Fetch(context.Context) ([]byte, error) { return nil, r.err }

func (failingReader) Parse([]byte) ([]model.Channel, []model.Programme, error) {
	return nil, nil, nil
}

type staticReader struct{}

func (staticReader) Fetch(context.Context) ([]byte, error) { return []byte("RAW"), nil }

func (staticReader) Parse([]byte) ([]model.Channel, []model.Programme, error) {
	start := time.Now().UTC()
	return []model.Channel{{
			ID:        "news",
			Name:      "News",
			StreamURL: "https://example.test/news.m3u8",
		}}, []model.Programme{{
			ChannelID: "news",
			Title:     "News",
			Start:     start,
			Stop:      start.Add(time.Hour),
		}}, nil
}

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
	reader := lg.New(settings, httpx.NewClient(5*time.Second, 0))
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
	if status := f.Status(); status.LastError != "" || status.LastAttemptAt.IsZero() {
		t.Fatalf("successful refresh status: %+v", status)
	}
	if raw, err := cc.ReadRaw(model.ProviderLG); err != nil || len(raw) == 0 {
		t.Fatalf("raw not archived: %d bytes, %v", len(raw), err)
	}
}

func TestRefreshFailureKeepsLastGoodAndPersistsStatus(t *testing.T) {
	fetchErr := errors.New("upstream unavailable")
	settings := model.ProviderSettings{ID: model.ProviderLG, MinChannels: 1}
	reg := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{model.ProviderLG: failingReader{err: fetchErr}},
		map[model.ProviderID]model.ProviderSettings{model.ProviderLG: settings},
	)
	feed, _ := reg.Feed(model.ProviderLG)
	old := provider.Lineup{
		Channels:     []model.Channel{{NormalizedID: "old"}},
		ChannelCount: 1,
		FetchedAt:    time.Now().Add(-time.Hour),
	}
	feed.Set(old)
	cc := cache.New(t.TempDir())
	notified := 0
	pr := &providerRefresher{feed: feed, cache: cc, notify: func() { notified++ }}

	if err := pr.Refresh(context.Background()); !errors.Is(err, fetchErr) {
		t.Fatalf("Refresh error = %v, want %v", err, fetchErr)
	}
	if got := feed.Lineup(); len(got.Channels) != 1 || got.Channels[0].NormalizedID != "old" || !got.FetchedAt.Equal(old.FetchedAt) {
		t.Fatalf("last-good changed: %+v", got)
	}
	if notified != 0 {
		t.Fatalf("notified %d times after failure", notified)
	}
	status := feed.Status()
	if status.LastAttemptAt.IsZero() || status.LastError != fetchErr.Error() || status.LastErrorAt.IsZero() {
		t.Fatalf("feed status: %+v", status)
	}
	persisted, ok := cc.LoadStatus(model.ProviderLG)
	if !ok || persisted.LastError != fetchErr.Error() {
		t.Fatalf("persisted status: %+v ok=%v", persisted, ok)
	}
}

func TestCacheFailureKeepsLastGoodAndDoesNotNotify(t *testing.T) {
	settings := model.ProviderSettings{ID: model.ProviderLG, MinChannels: 1}
	reg := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{model.ProviderLG: staticReader{}},
		map[model.ProviderID]model.ProviderSettings{model.ProviderLG: settings},
	)
	feed, _ := reg.Feed(model.ProviderLG)
	old := provider.Lineup{Channels: []model.Channel{{NormalizedID: "old"}}, FetchedAt: time.Now().Add(-time.Hour)}
	feed.Set(old)

	cachePath := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(cachePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	notified := 0
	pr := &providerRefresher{feed: feed, cache: cache.New(cachePath), notify: func() { notified++ }}
	if err := pr.Refresh(context.Background()); err == nil {
		t.Fatal("expected cache publication failure")
	}
	if got := feed.Lineup(); got.Channels[0].NormalizedID != "old" || !got.FetchedAt.Equal(old.FetchedAt) {
		t.Fatalf("last-good changed: %+v", got)
	}
	if notified != 0 {
		t.Fatalf("notified %d times after cache failure", notified)
	}
}

func TestRestoreRehydratesFromRaw(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "provider", "lg", "testdata", "schedulelist.json"))
	if err != nil {
		t.Fatal(err)
	}
	cc := cache.New(t.TempDir())

	fetchedAt := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	meta := provider.Meta{
		FetchedAt:       fetchedAt,
		Classifications: map[string]model.Classification{"ch-news": model.ClassBeacon},
	}
	// Channels/programmes are not persisted; Restore re-parses raw.
	if err := cc.CommitProvider(model.ProviderLG, fixture, nil, nil, meta); err != nil {
		t.Fatal(err)
	}
	status := provider.Status{
		LastAttemptAt: fetchedAt.Add(30 * time.Minute),
		LastError:     "temporary failure",
		LastErrorAt:   fetchedAt.Add(30 * time.Minute),
	}
	if err := cc.WriteStatus(model.ProviderLG, status); err != nil {
		t.Fatal(err)
	}

	settings := lg.DefaultSettings().Merge(model.ProviderSettings{MinChannels: 1})
	reg := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{model.ProviderLG: lg.New(settings, nil)},
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
	if restored := f.Status(); restored.LastError != status.LastError || !restored.LastAttemptAt.Equal(status.LastAttemptAt) {
		t.Fatalf("status not restored: %+v", restored)
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
