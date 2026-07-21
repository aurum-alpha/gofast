package refresh

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/provider/lg"
	"github.com/j27-aurum/gofast/internal/provider/published"
)

type failingReader struct {
	err error
}

func (r failingReader) Fetch(context.Context) (provider.Raw, error) { return nil, r.err }

func (failingReader) Parse(provider.Raw) ([]model.Channel, []model.Programme, error) {
	return nil, nil, nil
}

type staticReader struct{}

func (staticReader) Fetch(context.Context) (provider.Raw, error) {
	return provider.Raw{"fixture": []byte("RAW")}, nil
}

func (staticReader) Parse(provider.Raw) ([]model.Channel, []model.Programme, error) {
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

func TestApplyEmissionPolicyDropsDRM(t *testing.T) {
	got, _ := applyEmissionPolicy([]model.Channel{
		{ID: "native", Classification: model.ClassNative},
		{ID: "drm", Classification: model.ClassDRM, LicenseURL: "https://license.example"},
	}, EmissionPolicy{})
	if got[0].Excluded {
		t.Fatalf("native channel excluded: %+v", got[0])
	}
	if !got[1].Excluded || got[1].FilterReason != "DRM" {
		t.Fatalf("DRM channel not excluded: %+v", got[1])
	}
}

func TestTransformRejectsNormalizedIDCollision(t *testing.T) {
	settings := model.ProviderSettings{ID: model.ProviderLG}
	registry := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{model.ProviderLG: staticReader{}},
		map[model.ProviderID]model.ProviderSettings{model.ProviderLG: settings},
	)
	feed, _ := registry.Feed(model.ProviderLG)
	refresher := &providerRefresher{feed: feed}

	_, _, _, err := refresher.transform([]model.Channel{
		{ID: "a b", Name: "One", StreamURL: "https://stream.test/one.m3u8"},
		{ID: "a_b", Name: "Two", StreamURL: "https://stream.test/two.m3u8"},
	}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), `normalized channel id collision "a_b"`) {
		t.Fatalf("collision error: %v", err)
	}
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
	if raw, err := cc.ReadRaw(model.ProviderLG); err != nil || len(raw["schedule.json"]) == 0 {
		t.Fatalf("raw not archived: %d bytes, %v", len(raw), err)
	}
}

func TestSyntheticNumberPersistsAcrossRefreshAndRestore(t *testing.T) {
	settings := model.ProviderSettings{
		ID:                       model.ProviderXumo,
		Label:                    "Xumo",
		MinChannels:              1,
		SynthesizeChannelNumbers: 5000,
	}
	newRegistry := func() *provider.Registry {
		return provider.NewRegistry(
			map[model.ProviderID]provider.Reader{model.ProviderXumo: staticReader{}},
			map[model.ProviderID]model.ProviderSettings{model.ProviderXumo: settings},
		)
	}
	cc := cache.New(t.TempDir())
	registry := newRegistry()
	feed, _ := registry.Feed(model.ProviderXumo)
	if err := (&providerRefresher{feed: feed, cache: cc}).Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := feed.Channels()[0].OffsetNumber; got != 5000 {
		t.Fatalf("synthetic number = %d, want 5000", got)
	}
	meta, ok := cc.LoadMeta(model.ProviderXumo)
	if !ok || meta.SyntheticChannelNumbers["news"] != 5000 {
		t.Fatalf("persisted meta: %+v ok=%v", meta, ok)
	}

	restored := newRegistry()
	Restore(restored, cc, EmissionPolicy{})
	restoredFeed, _ := restored.Feed(model.ProviderXumo)
	if got := restoredFeed.Channels()[0].OffsetNumber; got != 5000 {
		t.Fatalf("restored number = %d, want 5000", got)
	}
}

func TestFailedGateDoesNotConsumeSyntheticNumber(t *testing.T) {
	settings := model.ProviderSettings{
		ID:                       model.ProviderXumo,
		MinChannels:              2,
		SynthesizeChannelNumbers: 5000,
	}
	registry := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{model.ProviderXumo: staticReader{}},
		map[model.ProviderID]model.ProviderSettings{model.ProviderXumo: settings},
	)
	feed, _ := registry.Feed(model.ProviderXumo)
	feed.Set(provider.Lineup{
		SyntheticChannelNumbers: provider.ChannelNumberAssignments{"gone": 5000},
		FetchedAt:               time.Now().Add(-time.Hour),
	})
	cc := cache.New(t.TempDir())
	if err := (&providerRefresher{feed: feed, cache: cc}).Refresh(context.Background()); err == nil {
		t.Fatal("expected min_channels gate failure")
	}
	got := feed.Lineup().SyntheticChannelNumbers
	if len(got) != 1 || got["gone"] != 5000 {
		t.Fatalf("failed candidate changed assignments: %+v", got)
	}
	if _, ok := cc.LoadMeta(model.ProviderXumo); ok {
		t.Fatal("failed candidate published metadata")
	}
}

func TestPublishedProviderRefreshAndRestore(t *testing.T) {
	playlist, err := os.ReadFile(filepath.Join("..", "provider", "published", "testdata", "distrotv.m3u"))
	if err != nil {
		t.Fatal(err)
	}
	guide, err := os.ReadFile(filepath.Join("..", "provider", "published", "testdata", "distrotv.xml"))
	if err != nil {
		t.Fatal(err)
	}
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	if _, err := gzipWriter.Write(guide); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/playlist":
			_, _ = w.Write(playlist)
		case "/guide":
			_, _ = w.Write(compressed.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	settings := model.ProviderSettings{
		ID:          model.ProviderDistroTV,
		Label:       "DistroTV",
		MinChannels: 1,
		M3UURL:      server.URL + "/playlist",
		EPGURL:      server.URL + "/guide",
	}
	source := published.Source{ID: model.ProviderDistroTV, EPGGzip: true}
	reader := published.New(source, settings, httpx.NewClient(5*time.Second, 1))
	registry := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{model.ProviderDistroTV: reader},
		map[model.ProviderID]model.ProviderSettings{model.ProviderDistroTV: settings},
	)
	feed, _ := registry.Feed(model.ProviderDistroTV)
	cc := cache.New(t.TempDir())
	refresher := &providerRefresher{feed: feed, cache: cc}
	if err := refresher.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if channels := feed.Channels(); len(channels) != 2 || channels[0].NormalizedID != "dtv_EPGACE_TV" {
		t.Fatalf("channels: %+v", channels)
	}
	raw, err := cc.ReadRaw(model.ProviderDistroTV)
	if err != nil || len(raw[published.RawPlaylist]) == 0 || len(raw[published.RawGuideGzip]) == 0 {
		t.Fatalf("raw=%v err=%v", raw, err)
	}
	m3uData, _ := cc.ReadM3U(model.ProviderDistroTV)
	xmlData, _ := cc.ReadXMLTV(model.ProviderDistroTV)
	if !strings.Contains(string(m3uData), `tvg-id="dtv_EPGACE_TV"`) ||
		!strings.Contains(string(xmlData), `channel="dtv_EPGACE_TV"`) {
		t.Fatalf("m3u=%s xml=%s", m3uData, xmlData)
	}

	restoredReader := published.New(source, settings, nil)
	restoredRegistry := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{model.ProviderDistroTV: restoredReader},
		map[model.ProviderID]model.ProviderSettings{model.ProviderDistroTV: settings},
	)
	Restore(restoredRegistry, cc, EmissionPolicy{})
	restoredFeed, _ := restoredRegistry.Feed(model.ProviderDistroTV)
	if channels := restoredFeed.Channels(); len(channels) != 2 || channels[0].NormalizedID != "dtv_EPGACE_TV" {
		t.Fatalf("restored channels: %+v", channels)
	}
}

func TestPublishedGuideFetchFailureKeepsLastGoodGeneration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/playlist" {
			_, _ = w.Write([]byte("#EXTM3U\n"))
			return
		}
		http.Error(w, "guide unavailable", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)
	settings := model.ProviderSettings{
		ID:          model.ProviderDistroTV,
		MinChannels: 1,
		M3UURL:      server.URL + "/playlist",
		EPGURL:      server.URL + "/guide",
	}
	reader := published.New(published.Source{ID: model.ProviderDistroTV, EPGGzip: true}, settings, httpx.NewClient(time.Second, 1))
	registry := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{model.ProviderDistroTV: reader},
		map[model.ProviderID]model.ProviderSettings{model.ProviderDistroTV: settings},
	)
	feed, _ := registry.Feed(model.ProviderDistroTV)
	old := provider.Lineup{
		Channels:     []model.Channel{{NormalizedID: "old"}},
		ChannelCount: 1,
		FetchedAt:    time.Now().Add(-time.Hour),
	}
	feed.Set(old)
	cc := cache.New(t.TempDir())
	oldRaw := provider.Raw{
		published.RawPlaylist:  []byte("OLD-PLAYLIST"),
		published.RawGuideGzip: []byte("OLD-GUIDE"),
	}
	if err := cc.CommitProvider(model.ProviderDistroTV, oldRaw, cache.M3U("OLD-M3U"), cache.XMLTV("OLD-XML"), provider.Meta{}); err != nil {
		t.Fatal(err)
	}

	refresher := &providerRefresher{feed: feed, cache: cc}
	if err := refresher.Refresh(context.Background()); err == nil {
		t.Fatal("guide fetch failure should fail refresh")
	}
	if got := feed.Lineup(); len(got.Channels) != 1 || got.Channels[0].NormalizedID != "old" {
		t.Fatalf("last-good feed changed: %+v", got)
	}
	raw, err := cc.ReadRaw(model.ProviderDistroTV)
	if err != nil || string(raw[published.RawPlaylist]) != "OLD-PLAYLIST" || string(raw[published.RawGuideGzip]) != "OLD-GUIDE" {
		t.Fatalf("last-good generation changed: raw=%q err=%v", raw, err)
	}
}

func TestRefreshFailureKeepsLastGoodAndPersistsStatus(t *testing.T) {
	fetchErr := errors.New("upstream unavailable")
	settings := model.ProviderSettings{ID: model.ProviderPluto, MinChannels: 1}
	reg := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{model.ProviderPluto: failingReader{err: fetchErr}},
		map[model.ProviderID]model.ProviderSettings{model.ProviderPluto: settings},
	)
	feed, _ := reg.Feed(model.ProviderPluto)
	old := provider.Lineup{
		Channels:     []model.Channel{{NormalizedID: "old"}},
		ChannelCount: 1,
		FetchedAt:    time.Now().Add(-time.Hour),
	}
	feed.Set(old)
	cc := cache.New(t.TempDir())
	oldRaw := provider.Raw{
		"channels.json.gz": []byte("OLD-CHANNELS"),
		"guide.xml.gz":     []byte("OLD-GUIDE"),
	}
	if err := cc.CommitProvider(model.ProviderPluto, oldRaw, cache.M3U("OLD-M3U"), cache.XMLTV("OLD-XML"), provider.Meta{}); err != nil {
		t.Fatal(err)
	}
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
	raw, err := cc.ReadRaw(model.ProviderPluto)
	if err != nil || string(raw["channels.json.gz"]) != "OLD-CHANNELS" || string(raw["guide.xml.gz"]) != "OLD-GUIDE" {
		t.Fatalf("last-good generation changed: raw=%q err=%v", raw, err)
	}
	status := feed.Status()
	if status.LastAttemptAt.IsZero() || status.LastError != fetchErr.Error() || status.LastErrorAt.IsZero() {
		t.Fatalf("feed status: %+v", status)
	}
	persisted, ok := cc.LoadStatus(model.ProviderPluto)
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
	if err := cc.CommitProvider(model.ProviderLG, provider.Raw{"schedule.json": fixture}, nil, nil, meta); err != nil {
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

	Restore(reg, cc, EmissionPolicy{})

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
