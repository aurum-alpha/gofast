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

func TestHistoryNewestFirst(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	at1 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	at2 := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	v1, _ := json.Marshal(model.ChannelHealth{Status: model.HealthDegraded, LastCheck: model.HealthCheckFailure})
	v2, _ := json.Marshal(model.ChannelHealth{Status: model.HealthHealthy, LastCheck: model.HealthCheckSuccess})
	for _, ev := range []Event{
		{Provider: model.ProviderLG, ChannelID: "news", Kind: KindHealth, Value: v1, At: at1, Source: "health_l1"},
		{Provider: model.ProviderLG, ChannelID: "news", Kind: KindHealth, Value: v2, At: at2, Source: "health_l2"},
	} {
		if err := store.Handle(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.History(ctx, model.ProviderLG, "news", KindHealth, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
	if !got[0].At.Equal(at2) || got[0].Source != "health_l2" {
		t.Fatalf("first=%+v", got[0])
	}
	if !got[1].At.Equal(at1) {
		t.Fatalf("second=%+v", got[1])
	}
}

func TestSuccessRate(t *testing.T) {
	at := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	okVal, _ := json.Marshal(model.ChannelHealth{LastCheck: model.HealthCheckSuccess})
	failVal, _ := json.Marshal(model.ChannelHealth{LastCheck: model.HealthCheckFailure})
	events := []HistoryEvent{
		{At: at, Value: okVal},
		{At: at.Add(time.Hour), Value: failVal},
		{At: at.Add(2 * time.Hour), Value: okVal},
		{At: at.Add(-48 * time.Hour), Value: failVal}, // outside window
	}
	rate, ok := SuccessRate(events, at.Add(-time.Hour))
	if !ok || rate != 2.0/3.0 {
		t.Fatalf("rate=%v ok=%v", rate, ok)
	}
	_, ok = SuccessRate(nil, at)
	if ok {
		t.Fatal("empty should be !ok")
	}
}

func TestAnnotatePaintsClassificationWhenEmpty(t *testing.T) {
	store := openTestStore(t)
	v, _ := json.Marshal(model.Classification("BEACON")) // legacy wire value
	if err := store.Handle(context.Background(), Event{
		Provider:  model.ProviderLG,
		ChannelID: "ch-1",
		Kind:      KindClassification,
		Value:     v,
		At:        time.Now().UTC(),
		Source:    "classifier",
	}); err != nil {
		t.Fatal(err)
	}

	out := store.Annotate(model.ProviderLG, []model.Channel{
		{NormalizedID: "ch-1", Name: "One"},
		{NormalizedID: "ch-2", Name: "Two", Classification: model.ClassNative},
	})
	if out[0].Classification != model.ClassAmagiSSAI {
		t.Fatalf("ch-1 class: %q want AMAGI_SSAI (canonicalized from BEACON)", out[0].Classification)
	}
	if out[1].Classification != model.ClassNative {
		t.Fatalf("ch-2 should keep in-memory class: %q", out[1].Classification)
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

func TestPresenceCurrentAndEventsSince(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	at1 := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	at2 := time.Date(2026, 7, 1, 13, 0, 0, 0, time.UTC)
	at3 := time.Date(2026, 7, 1, 14, 0, 0, 0, time.UTC)

	presentNews, _ := json.Marshal(Presence{State: PresencePresent, Name: "News", TvgID: "news"})
	if err := store.Handle(ctx, Event{
		Provider: model.ProviderLG, ChannelID: "news", Kind: KindPresence,
		Value: presentNews, At: at1, Source: "refresh",
	}); err != nil {
		t.Fatal(err)
	}
	classNative, _ := json.Marshal(model.ClassNative)
	if err := store.Handle(ctx, Event{
		Provider: model.ProviderLG, ChannelID: "news", Kind: KindClassification,
		Value: classNative, At: at2, Source: "classifier",
	}); err != nil {
		t.Fatal(err)
	}
	absentNews, _ := json.Marshal(Presence{State: PresenceAbsent, Name: "News", TvgID: "news"})
	if err := store.Handle(ctx, Event{
		Provider: model.ProviderLG, ChannelID: "news", Kind: KindPresence,
		Value: absentNews, At: at3, Source: "refresh",
	}); err != nil {
		t.Fatal(err)
	}
	// Different provider / health noise should not appear in default EventsSince.
	health, _ := json.Marshal(model.ChannelHealth{Status: model.HealthHealthy})
	if err := store.Handle(ctx, Event{
		Provider: model.ProviderXumo, ChannelID: "other", Kind: KindHealth,
		Value: health, At: at2, Source: "probe",
	}); err != nil {
		t.Fatal(err)
	}

	cur := store.CurrentPresence(model.ProviderLG)
	if len(cur) != 1 {
		t.Fatalf("CurrentPresence: %+v", cur)
	}
	if cur["news"].State != PresenceAbsent || cur["news"].Name != "News" {
		t.Fatalf("want absent News, got %+v", cur["news"])
	}

	events, err := store.EventsSince(ctx, at1, nil, model.ProviderLG, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("EventsSince len=%d want 3: %+v", len(events), events)
	}
	if events[0].Kind != KindPresence || events[1].Kind != KindClassification || events[2].Kind != KindPresence {
		t.Fatalf("order/kinds: %+v", events)
	}
	if !events[0].At.Equal(at1) || !events[2].At.Equal(at3) {
		t.Fatalf("want oldest→newest, got %v then %v", events[0].At, events[2].At)
	}

	onlyPresence, err := store.EventsSince(ctx, at1, []Kind{KindPresence}, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(onlyPresence) != 2 {
		t.Fatalf("presence-only: got %d", len(onlyPresence))
	}
}

func TestPruneDropsOldEventsKeepsCurrent(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	oldAt := time.Now().UTC().Add(-100 * 24 * time.Hour)
	newAt := time.Now().UTC()

	oldVal, _ := json.Marshal(Presence{State: PresencePresent, Name: "Old"})
	if err := store.Handle(ctx, Event{
		Provider: model.ProviderLG, ChannelID: "ch", Kind: KindPresence,
		Value: oldVal, At: oldAt, Source: "refresh",
	}); err != nil {
		t.Fatal(err)
	}
	// Force prune due on next Handle.
	store.mu.Lock()
	store.lastPrune = time.Time{}
	store.mu.Unlock()

	newVal, _ := json.Marshal(Presence{State: PresenceAbsent, Name: "Old"})
	if err := store.Handle(ctx, Event{
		Provider: model.ProviderLG, ChannelID: "ch", Kind: KindPresence,
		Value: newVal, At: newAt, Source: "refresh",
	}); err != nil {
		t.Fatal(err)
	}

	n, err := store.EventCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("after prune want 1 event, got %d", n)
	}
	raw, ok := store.Current(model.ProviderLG, "ch", KindPresence)
	if !ok {
		t.Fatal("current must survive prune")
	}
	var p Presence
	if err := json.Unmarshal(raw, &p); err != nil || p.State != PresenceAbsent {
		t.Fatalf("current: %s err=%v", raw, err)
	}
}
