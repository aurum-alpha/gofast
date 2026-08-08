package aggregate_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/aggregate"
	"github.com/j27-aurum/gofast/internal/cache"
	"github.com/j27-aurum/gofast/internal/m3u"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
	"github.com/j27-aurum/gofast/internal/xmltv"
)

type stubReader struct{}

func (stubReader) Fetch(context.Context) (provider.Raw, error) { return nil, nil }

func (stubReader) Parse(provider.Raw) ([]model.Channel, []model.Programme, error) {
	return nil, nil, nil
}

func TestRebuildWritesNamespacedAggregate(t *testing.T) {
	cc := cache.New(t.TempDir())
	reg := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{"lg": stubReader{}},
		map[model.ProviderID]model.ProviderSettings{"lg": {ID: "lg", Label: "LG"}},
	)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f, _ := reg.Feed("lg")
	f.Set(provider.Lineup{
		Channels:   []model.Channel{{Provider: "lg", NormalizedID: "news", Name: "News", OffsetNumber: 1005, StreamURL: "https://s"}},
		Programmes: []model.Programme{{ChannelID: "news", Title: "Morning", Start: start, Stop: start.Add(time.Hour)}},
	})

	agg := aggregate.New(reg, cc)
	if err := agg.Rebuild(); err != nil {
		t.Fatal(err)
	}

	m, err := cc.ReadAggregateM3U()
	if err != nil || !strings.Contains(string(m), `tvg-id="lg.news"`) {
		t.Fatalf("aggregate m3u (namespaced): %q %v", m, err)
	}
	x, err := cc.ReadAggregateXMLTV()
	if err != nil || !strings.Contains(string(x), `id="lg.news"`) || !strings.Contains(string(x), `channel="lg.news"`) {
		t.Fatalf("aggregate xml (namespaced): %q %v", x, err)
	}

	playlist, err := m3u.Parse(bytes.NewReader(m))
	if err != nil {
		t.Fatal(err)
	}
	guide, err := xmltv.Parse(bytes.NewReader(x))
	if err != nil {
		t.Fatal(err)
	}
	playlistIDs := make(map[string]struct{}, len(playlist))
	for _, channel := range playlist {
		playlistIDs[channel.ID] = struct{}{}
	}
	if len(playlistIDs) != len(guide.ChannelIDs) {
		t.Fatalf("playlist ids=%v guide ids=%v", playlistIDs, guide.ChannelIDs)
	}
	for _, id := range guide.ChannelIDs {
		if _, ok := playlistIDs[id]; !ok {
			t.Fatalf("guide channel %q missing from playlist", id)
		}
	}
}

func TestEmptyRebuildPreservesExistingAggregate(t *testing.T) {
	cc := cache.New(t.TempDir())
	if err := cc.CommitAggregate(model.M3UFile("KEEP-M3U"), model.XMLTVFile("KEEP-XML")); err != nil {
		t.Fatal(err)
	}
	reg := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{"lg": stubReader{}},
		map[model.ProviderID]model.ProviderSettings{"lg": {ID: "lg"}},
	)
	agg := aggregate.New(reg, cc)
	if err := agg.Rebuild(); !errors.Is(err, aggregate.ErrEmptyAggregate) {
		t.Fatalf("Rebuild error = %v, want ErrEmptyAggregate", err)
	}
	m, err := cc.ReadAggregateM3U()
	if err != nil || string(m) != "KEEP-M3U" {
		t.Fatalf("aggregate m3u overwritten: %q %v", m, err)
	}
	x, err := cc.ReadAggregateXMLTV()
	if err != nil || string(x) != "KEEP-XML" {
		t.Fatalf("aggregate xml overwritten: %q %v", x, err)
	}
}

func TestNotifyCoalesces(t *testing.T) {
	cc := cache.New(t.TempDir())
	reg := provider.NewRegistry(
		map[model.ProviderID]provider.Reader{"lg": stubReader{}},
		map[model.ProviderID]model.ProviderSettings{"lg": {ID: "lg"}},
	)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f, _ := reg.Feed("lg")
	f.Set(provider.Lineup{
		Channels:   []model.Channel{{Provider: "lg", NormalizedID: "news", Name: "News", StreamURL: "https://s"}},
		Programmes: []model.Programme{{ChannelID: "news", Title: "Morning", Start: start, Stop: start.Add(time.Hour)}},
	})
	agg := aggregate.New(reg, cc)
	// Many notifies without a running loop collapse into at most one pending.
	for i := 0; i < 100; i++ {
		agg.Notify()
	}
	// Run briefly to drain the single pending signal, then cancel.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	agg.Run(ctx)
	if _, err := cc.ReadAggregateM3U(); err != nil {
		t.Fatalf("expected aggregate written after Notify+Run: %v", err)
	}
}
