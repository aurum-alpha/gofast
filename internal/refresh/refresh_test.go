package refresh

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/httpx"
	"github.com/j27-aurum/gofast/internal/logocache"
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

type seqReader struct {
	n     int
	steps []struct {
		chs   []model.Channel
		progs []model.Programme
	}
}

func (r *seqReader) Fetch(context.Context) (provider.Raw, error) {
	return provider.Raw{"fixture": []byte("RAW")}, nil
}

func (r *seqReader) Parse(provider.Raw) ([]model.Channel, []model.Programme, error) {
	i := r.n
	if i >= len(r.steps) {
		i = len(r.steps) - 1
	}
	step := r.steps[i]
	r.n++
	return step.chs, step.progs, nil
}

func goodLineup() ([]model.Channel, []model.Programme) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	return []model.Channel{{
			ID:        "news",
			Name:      "News",
			StreamURL: "https://example.test/news.m3u8",
		}, {
			ID:        "sports",
			Name:      "Sports",
			StreamURL: "https://example.test/sports.m3u8",
		}}, []model.Programme{{
			ChannelID: "news",
			Title:     "News Hour",
			Start:     start,
			Stop:      start.Add(time.Hour),
		}, {
			ChannelID: "sports",
			Title:     "Sports Hour",
			Start:     start,
			Stop:      start.Add(time.Hour),
		}}
}

func TestGuttedLineupKeepsLastGoodHTTPtest(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "provider", "lg", "testdata", "schedulelist.json"))
	if err != nil {
		t.Fatal(err)
	}
	gutted := []byte(`{"categories":[{"channels":[{
		"channelId":"ch-only","channelName":"Only","channelNumber":"1",
		"mediaStaticUrl":"https://stream.example/only.m3u8",
		"programs":[{"programTitle":"P","startDateTime":"2024-06-01T12:00:00Z","endDateTime":"2024-06-01T13:00:00Z"}]
	}]}]}`)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_, _ = w.Write(body)
			return
		}
		_, _ = w.Write(gutted)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	cc := cache.New(dir)
	settings := lg.DefaultSettings().Merge(model.ProviderSettings{MinChannels: 2, ChannelsURL: srv.URL})
	reader := lg.New(settings, httpx.NewClient(5*time.Second, 0))
	reg := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{model.ProviderLG: reader},
		map[model.ProviderID]model.ProviderSettings{model.ProviderLG: settings},
	)
	feed, _ := reg.Feed(model.ProviderLG)
	notified := 0
	pr := &providerRefresher{feed: feed, cache: cc, notify: func() { notified++ }}
	if err := pr.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	good := feed.Lineup()
	goodRaw, err := cc.ReadRaw(model.ProviderLG)
	if err != nil {
		t.Fatal(err)
	}
	goodM3U, _ := cc.ReadM3U(model.ProviderLG)
	goodXML, _ := cc.ReadXMLTV(model.ProviderLG)
	currentBefore, err := os.ReadFile(filepath.Join(dir, "lg", "current"))
	if err != nil {
		t.Fatal(err)
	}

	if err := pr.Refresh(context.Background()); err == nil {
		t.Fatal("expected min_channels gate failure on gutted lineup")
	}
	if got := feed.Lineup(); len(got.Channels) != len(good.Channels) || !got.FetchedAt.Equal(good.FetchedAt) {
		t.Fatalf("last-good lineup changed: %+v", got)
	}
	raw, err := cc.ReadRaw(model.ProviderLG)
	if err != nil || string(raw["schedule.json"]) != string(goodRaw["schedule.json"]) {
		t.Fatalf("last-good raw changed")
	}
	m3uData, _ := cc.ReadM3U(model.ProviderLG)
	xmlData, _ := cc.ReadXMLTV(model.ProviderLG)
	if string(m3uData) != string(goodM3U) || string(xmlData) != string(goodXML) {
		t.Fatalf("last-good artifacts changed")
	}
	currentAfter, _ := os.ReadFile(filepath.Join(dir, "lg", "current"))
	if string(currentAfter) != string(currentBefore) {
		t.Fatalf("current pointer moved: %q -> %q", currentBefore, currentAfter)
	}
	if notified != 1 {
		t.Fatalf("notify count = %d, want 1", notified)
	}
	if status := feed.Status(); !strings.Contains(status.LastError, "min_channels") {
		t.Fatalf("status error: %+v", status)
	}
}

func TestProgrammeCountGateKeepsLastGood(t *testing.T) {
	chs, progs := goodLineup()
	reader := &seqReader{steps: []struct {
		chs   []model.Channel
		progs []model.Programme
	}{
		{chs, progs},
		{chs, nil},
	}}
	settings := model.ProviderSettings{ID: model.ProviderXumo, MinChannels: 1}
	reg := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{model.ProviderXumo: reader},
		map[model.ProviderID]model.ProviderSettings{model.ProviderXumo: settings},
	)
	feed, _ := reg.Feed(model.ProviderXumo)
	cc := cache.New(t.TempDir())
	notified := 0
	pr := &providerRefresher{feed: feed, cache: cc, notify: func() { notified++ }}
	if err := pr.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	good := feed.Lineup()
	goodM3U, _ := cc.ReadM3U(model.ProviderXumo)
	goodXML, _ := cc.ReadXMLTV(model.ProviderXumo)

	if err := pr.Refresh(context.Background()); err == nil || !strings.Contains(err.Error(), "no exportable programmes") {
		t.Fatalf("programme gate error: %v", err)
	}
	if got := feed.Lineup(); !got.FetchedAt.Equal(good.FetchedAt) || len(got.Channels) != len(good.Channels) {
		t.Fatalf("last-good changed: %+v", got)
	}
	m3uData, _ := cc.ReadM3U(model.ProviderXumo)
	xmlData, _ := cc.ReadXMLTV(model.ProviderXumo)
	if string(m3uData) != string(goodM3U) || string(xmlData) != string(goodXML) {
		t.Fatal("disk last-good changed")
	}
	if notified != 1 {
		t.Fatalf("notify = %d", notified)
	}
}

func TestXMLValidateFailureKeepsLastGood(t *testing.T) {
	chs, progs := goodLineup()
	badProgs := make([]model.Programme, len(progs))
	copy(badProgs, progs)
	far := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range badProgs {
		badProgs[i].Start = far
		badProgs[i].Stop = far.Add(time.Hour)
	}
	reader := &seqReader{steps: []struct {
		chs   []model.Channel
		progs []model.Programme
	}{
		{chs, progs},
		{chs, badProgs},
	}}
	settings := model.ProviderSettings{ID: model.ProviderXumo, MinChannels: 1}
	reg := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{model.ProviderXumo: reader},
		map[model.ProviderID]model.ProviderSettings{model.ProviderXumo: settings},
	)
	feed, _ := reg.Feed(model.ProviderXumo)
	cc := cache.New(t.TempDir())
	notified := 0
	pr := &providerRefresher{feed: feed, cache: cc, notify: func() { notified++ }}
	if err := pr.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	good := feed.Lineup()
	goodM3U, _ := cc.ReadM3U(model.ProviderXumo)
	goodXML, _ := cc.ReadXMLTV(model.ProviderXumo)

	if err := pr.Refresh(context.Background()); err == nil {
		t.Fatal("expected xmltv validate failure")
	}
	if got := feed.Lineup(); !got.FetchedAt.Equal(good.FetchedAt) {
		t.Fatalf("last-good changed: %+v", got)
	}
	m3uData, _ := cc.ReadM3U(model.ProviderXumo)
	xmlData, _ := cc.ReadXMLTV(model.ProviderXumo)
	if string(m3uData) != string(goodM3U) || string(xmlData) != string(goodXML) {
		t.Fatal("disk last-good changed")
	}
	if notified != 1 {
		t.Fatalf("notify = %d", notified)
	}
}

func TestJitterBounds(t *testing.T) {
	d := 10 * time.Minute
	lo := d - d/10
	hi := d + d/10
	for i := 0; i < 500; i++ {
		j := jitter(d)
		if j < lo || j > hi {
			t.Fatalf("jitter(%v)=%v outside [%v,%v]", d, j, lo, hi)
		}
	}
	for i := 0; i < 50; i++ {
		if got := jitter(30 * time.Second); got != minInterval {
			t.Fatalf("jitter below minInterval floor: got %v want %v", got, minInterval)
		}
	}
}

type scheduleStub struct {
	id        model.ProviderID
	interval  time.Duration
	fetchedAt time.Time
	refresh   func(context.Context) error
}

func (s scheduleStub) ID() model.ProviderID    { return s.id }
func (s scheduleStub) Interval() time.Duration { return s.interval }
func (s scheduleStub) FetchedAt() time.Time    { return s.fetchedAt }
func (s scheduleStub) Refresh(ctx context.Context) error {
	if s.refresh != nil {
		return s.refresh(ctx)
	}
	return nil
}

func TestNextRefreshHeartbeat(t *testing.T) {
	prev := scheduleHeartbeat
	scheduleHeartbeat = 25 * time.Millisecond
	t.Cleanup(func() { scheduleHeartbeat = prev })

	var buf bytes.Buffer
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prevLogger) })

	stub := scheduleStub{
		id:        model.ProviderLG,
		interval:  time.Hour,
		fetchedAt: time.Now(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		run(ctx, stub)
		close(done)
	}()

	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		if strings.Contains(buf.String(), `"msg":"refresh schedule"`) &&
			strings.Contains(buf.String(), `"provider":"lg"`) &&
			strings.Contains(buf.String(), `"next_refresh_at"`) &&
			strings.Contains(buf.String(), `"refresh_in"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("missing schedule log: %s", buf.String())
		}
		time.Sleep(10 * time.Millisecond)
	}

	beforeCancel := buf.Len()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("run did not stop")
	}
	time.Sleep(80 * time.Millisecond)
	if buf.Len() > beforeCancel+200 {
		t.Fatalf("heartbeat continued after cancel: grew %d bytes", buf.Len()-beforeCancel)
	}
}

type logoReader struct {
	logoURL string
}

func (r logoReader) Fetch(context.Context) (provider.Raw, error) {
	return provider.Raw{"fixture": []byte("RAW")}, nil
}

func (r logoReader) Parse(provider.Raw) ([]model.Channel, []model.Programme, error) {
	start := time.Now().UTC()
	return []model.Channel{{
			ID:        "news",
			Name:      "News",
			StreamURL: "https://example.test/news.m3u8",
			LogoURL:   r.logoURL,
		}}, []model.Programme{{
			ChannelID: "news",
			Title:     "News",
			Start:     start,
			Stop:      start.Add(time.Hour),
		}}, nil
}

func TestPrepareRewritesLogosWhenEnabled(t *testing.T) {
	var hits int
	logoSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png"))
	}))
	t.Cleanup(logoSrv.Close)

	settings := model.ProviderSettings{ID: model.ProviderLG, Label: "LG", MinChannels: 1}
	reg := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{model.ProviderLG: logoReader{logoURL: logoSrv.URL + "/a.png"}},
		map[model.ProviderID]model.ProviderSettings{model.ProviderLG: settings},
	)
	feed, _ := reg.Feed(model.ProviderLG)
	cc := cache.New(t.TempDir())
	logos := logocache.New(cc, logoSrv.Client(), "http://fastgen.lan:8180", time.Hour)
	pr := &providerRefresher{feed: feed, cache: cc}
	if err := pr.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hits != 0 {
		t.Fatalf("refresh should not hit artwork yet, hits=%d", hits)
	}
	pr.logos = logos
	if _, err := pr.rewriteLogosAndRepublish(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	m3uData, err := cc.ReadM3U(model.ProviderLG)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(m3uData), "http://fastgen.lan:8180/logos/lg/news.png") {
		t.Fatalf("m3u missing rewritten logo: %s", m3uData)
	}
	if hits != 1 {
		t.Fatalf("logo hits=%d", hits)
	}
	if got := feed.Channels()[0].LogoURL; got != "http://fastgen.lan:8180/logos/lg/news.png" {
		t.Fatalf("api logo_url=%q", got)
	}
}

func TestPrepareKeepsUpstreamLogosWhenDisabled(t *testing.T) {
	upstream := "https://cdn.example/logo.png"
	settings := model.ProviderSettings{ID: model.ProviderLG, Label: "LG", MinChannels: 1}
	reg := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{model.ProviderLG: logoReader{logoURL: upstream}},
		map[model.ProviderID]model.ProviderSettings{model.ProviderLG: settings},
	)
	feed, _ := reg.Feed(model.ProviderLG)
	pr := &providerRefresher{feed: feed, cache: cache.New(t.TempDir())}
	if err := pr.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	m3uData, err := pr.cache.ReadM3U(model.ProviderLG)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(m3uData), upstream) {
		t.Fatalf("expected upstream logo in m3u: %s", m3uData)
	}
	if got := feed.Channels()[0].LogoURL; got != upstream {
		t.Fatalf("logo_url=%q", got)
	}
}

func TestWarmLogosUpdatesStatus(t *testing.T) {
	var hits atomic.Int32
	logoSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(20 * time.Millisecond)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png"))
	}))
	t.Cleanup(logoSrv.Close)

	settings := model.ProviderSettings{ID: model.ProviderLG, Label: "LG", MinChannels: 1}
	reg := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{model.ProviderLG: logoReader{logoURL: logoSrv.URL + "/a.png"}},
		map[model.ProviderID]model.ProviderSettings{model.ProviderLG: settings},
	)
	feed, _ := reg.Feed(model.ProviderLG)
	cc := cache.New(t.TempDir())
	pr := &providerRefresher{feed: feed, cache: cc}
	if err := pr.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	Restore(reg, cc, EmissionPolicy{})
	if hits.Load() != 0 {
		t.Fatalf("restore must not download logos, hits=%d", hits.Load())
	}

	st := &Status{}
	logos := logocache.New(cc, logoSrv.Client(), "http://fastgen.lan:8180", time.Hour)
	done := make(chan struct{})
	go func() {
		WarmLogos(context.Background(), reg, cc, EmissionPolicy{}, logos, nil, st)
		close(done)
	}()
	deadline := time.After(2 * time.Second)
	sawRunning := false
	for !sawRunning {
		if st.Snapshot().Logos.Running {
			sawRunning = true
			break
		}
		select {
		case <-done:
			t.Fatal("WarmLogos finished before status showed running")
		case <-deadline:
			t.Fatal("timeout waiting for logos.running")
		case <-time.After(5 * time.Millisecond):
		}
	}
	<-done
	snap := st.Snapshot()
	if snap.Logos.Running || !snap.Ready || snap.Logos.Done != snap.Logos.Total {
		t.Fatalf("after warm: %+v", snap)
	}
	if hits.Load() < 1 {
		t.Fatalf("expected logo downloads, hits=%d", hits.Load())
	}
}
