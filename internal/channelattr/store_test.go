package channelattr

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
)

func TestHandleUpsertsCurrentAndAppendsHistory(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	at1 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	at2 := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)

	h1 := model.ChannelHealth{Status: model.HealthDegraded, ConsecutiveFailures: 1, LastCheck: model.HealthCheckFailure}
	v1, _ := json.Marshal(h1)
	if err := store.Handle(ctx, Event{
		Provider:  model.ProviderLG,
		ChannelID: "news",
		Kind:      KindHealth,
		Value:     v1,
		At:        at1,
		Source:    "probe",
	}); err != nil {
		t.Fatal(err)
	}

	h2 := model.ChannelHealth{Status: model.HealthDown, ConsecutiveFailures: 3, LastCheck: model.HealthCheckFailure}
	v2, _ := json.Marshal(h2)
	if err := store.Handle(ctx, Event{
		Provider:  model.ProviderLG,
		ChannelID: "news",
		Kind:      KindHealth,
		Value:     v2,
		At:        at2,
		Source:    "probe",
	}); err != nil {
		t.Fatal(err)
	}

	raw, ok := store.Current(model.ProviderLG, "news", KindHealth)
	if !ok {
		t.Fatal("expected current")
	}
	var got model.ChannelHealth
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != model.HealthDown || got.ConsecutiveFailures != 3 {
		t.Fatalf("current: %+v", got)
	}

	n, err := store.EventCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("events: got %d want 2", n)
	}
}

func TestLoadCurrentDoesNotRequireHistoryScan(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	v, _ := json.Marshal(model.ChannelHealth{Status: model.HealthHealthy})
	if err := store.Handle(ctx, Event{
		Provider:  model.ProviderLG,
		ChannelID: "a",
		Kind:      KindHealth,
		Value:     v,
		At:        time.Now().UTC(),
		Source:    "probe",
	}); err != nil {
		t.Fatal(err)
	}
	store.Close()

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, ok := reopened.Current(model.ProviderLG, "a", KindHealth); ok {
		t.Fatal("memory should be empty before LoadCurrent")
	}
	if err := reopened.LoadCurrent(); err != nil {
		t.Fatal(err)
	}
	raw, ok := reopened.Current(model.ProviderLG, "a", KindHealth)
	if !ok {
		t.Fatal("LoadCurrent should restore current only")
	}
	var h model.ChannelHealth
	if err := json.Unmarshal(raw, &h); err != nil || h.Status != model.HealthHealthy {
		t.Fatalf("got %s err=%v", raw, err)
	}
	// History still exists on disk but LoadCurrent never needed it.
	n, err := reopened.EventCount(ctx)
	if err != nil || n != 1 {
		t.Fatalf("history count=%d err=%v", n, err)
	}
}

func TestAnnotatePaintsHealth(t *testing.T) {
	store := openTestStore(t)
	v, _ := json.Marshal(model.ChannelHealth{
		Status:              model.HealthDegraded,
		ConsecutiveFailures: 2,
		LastCheck:           model.HealthCheckFailure,
	})
	if err := store.Handle(context.Background(), Event{
		Provider:  model.ProviderLG,
		ChannelID: "ch-1",
		Kind:      KindHealth,
		Value:     v,
		At:        time.Now().UTC(),
		Source:    "probe",
	}); err != nil {
		t.Fatal(err)
	}

	out := store.Annotate(model.ProviderLG, []model.Channel{
		{NormalizedID: "ch-1", Name: "One"},
		{NormalizedID: "ch-2", Name: "Two"},
	})
	if out[0].Health.Status != model.HealthDegraded || out[0].Health.ConsecutiveFailures != 2 {
		t.Fatalf("ch-1 health: %+v", out[0].Health)
	}
	if out[1].Health.Status != "" {
		t.Fatalf("ch-2 should remain unpainted: %+v", out[1].Health)
	}
}

func TestEmitReceiveRoundTrip(t *testing.T) {
	store := openTestStore(t)
	bus := NewBus(8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Receive(ctx, bus, store)

	v, _ := json.Marshal(model.ChannelHealth{Status: model.HealthHealthy, LastCheck: model.HealthCheckSuccess})
	if err := Emit(ctx, bus, Event{
		Provider:  model.ProviderPluto,
		ChannelID: "xyz",
		Kind:      KindHealth,
		Value:     v,
		At:        time.Now().UTC(),
		Source:    "probe",
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if raw, ok := store.Current(model.ProviderPluto, "xyz", KindHealth); ok {
			var h model.ChannelHealth
			if err := json.Unmarshal(raw, &h); err != nil {
				t.Fatal(err)
			}
			if h.Status != model.HealthHealthy {
				t.Fatalf("got %+v", h)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for AttrReceiver")
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
