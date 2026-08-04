package opsreport

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/channelattr"
	"github.com/j27-aurum/gofast/internal/model"
)

func TestPresenceDeltasNetChurn(t *testing.T) {
	store := openOpsAttrStore(t)
	ctx := context.Background()
	since := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)

	// Tubi-style churn: added then dropped in-window, ended where it started (absent) → omit.
	mustPresence(t, store, "tubi", "644786", "KING Seattle 5", channelattr.PresencePresent, since.Add(1*time.Hour))
	mustPresence(t, store, "tubi", "644786", "KING Seattle 5", channelattr.PresenceAbsent, since.Add(2*time.Hour))

	// Net added: only a present transition in the window.
	mustPresence(t, store, "tubi", "new", "New Channel", channelattr.PresencePresent, since.Add(3*time.Hour))

	// Net dropped: only an absent transition in the window.
	mustPresence(t, store, "lg", "gone", "Gone Channel", channelattr.PresenceAbsent, since.Add(4*time.Hour))

	// Present→absent→present: started present, ended present → omit.
	mustPresence(t, store, "lg", "stable", "Stable", channelattr.PresenceAbsent, since.Add(1*time.Hour))
	mustPresence(t, store, "lg", "stable", "Stable", channelattr.PresencePresent, since.Add(2*time.Hour))

	c := &Composer{Attrs: store}
	added, dropped, err := c.presenceDeltas(ctx, since, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0].ChannelID != "new" || added[0].Name != "New Channel" {
		t.Fatalf("added=%+v", added)
	}
	if len(dropped) != 1 || dropped[0].ChannelID != "gone" || dropped[0].Name != "Gone Channel" {
		t.Fatalf("dropped=%+v", dropped)
	}
	for _, row := range append(append([]DeltaRow{}, added...), dropped...) {
		switch row.ChannelID {
		case "644786", "stable":
			t.Fatalf("cancelled churn should be omitted: %+v", row)
		}
	}
	// Never both lists for the same id.
	seen := map[string]string{}
	for _, row := range added {
		seen[string(row.Provider)+"/"+row.ChannelID] = "added"
	}
	for _, row := range dropped {
		k := string(row.Provider) + "/" + row.ChannelID
		if seen[k] == "added" {
			t.Fatalf("%s in both added and dropped", k)
		}
	}
}

func TestClassAndHealthDeltasIncludeNames(t *testing.T) {
	store := openOpsAttrStore(t)
	ctx := context.Background()
	since := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)

	// Name lives on presence current (no registry).
	mustPresence(t, store, "plex", "ch1", "Friendly News", channelattr.PresencePresent, since.Add(-time.Hour))

	native, _ := json.Marshal(model.ClassNative)
	amagi, _ := json.Marshal(model.ClassAmagiSSAI)
	if err := store.Handle(ctx, channelattr.Event{
		Provider: "plex", ChannelID: "ch1", Kind: channelattr.KindClassification,
		Value: native, At: since.Add(-time.Hour), Source: "seed",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Handle(ctx, channelattr.Event{
		Provider: "plex", ChannelID: "ch1", Kind: channelattr.KindClassification,
		Value: amagi, At: since.Add(time.Hour), Source: "classifier",
	}); err != nil {
		t.Fatal(err)
	}

	healthy, _ := json.Marshal(model.ChannelHealth{Status: model.HealthHealthy})
	down, _ := json.Marshal(model.ChannelHealth{Status: model.HealthDown})
	if err := store.Handle(ctx, channelattr.Event{
		Provider: "plex", ChannelID: "ch1", Kind: channelattr.KindHealth,
		Value: healthy, At: since.Add(-time.Hour), Source: "probe",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Handle(ctx, channelattr.Event{
		Provider: "plex", ChannelID: "ch1", Kind: channelattr.KindHealth,
		Value: down, At: since.Add(2 * time.Hour), Source: "probe",
	}); err != nil {
		t.Fatal(err)
	}
	// Intermediate flip should net to healthy→down only.
	degraded, _ := json.Marshal(model.ChannelHealth{Status: model.HealthDegraded})
	if err := store.Handle(ctx, channelattr.Event{
		Provider: "plex", ChannelID: "ch1", Kind: channelattr.KindHealth,
		Value: degraded, At: since.Add(90 * time.Minute), Source: "probe",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Handle(ctx, channelattr.Event{
		Provider: "plex", ChannelID: "ch1", Kind: channelattr.KindHealth,
		Value: down, At: since.Add(3 * time.Hour), Source: "probe",
	}); err != nil {
		t.Fatal(err)
	}

	c := &Composer{Attrs: store}
	classRows, err := c.classDeltas(ctx, since, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(classRows) != 1 || classRows[0].Name != "Friendly News" {
		t.Fatalf("class=%+v", classRows)
	}
	if classRows[0].Old != string(model.ClassNative) || classRows[0].New != string(model.ClassAmagiSSAI) {
		t.Fatalf("class transition=%+v", classRows[0])
	}

	healthRows, err := c.healthDeltas(ctx, since, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(healthRows) != 1 || healthRows[0].Name != "Friendly News" {
		t.Fatalf("health=%+v", healthRows)
	}
	if healthRows[0].Old != model.HealthHealthy || healthRows[0].New != model.HealthDown {
		t.Fatalf("health transition=%+v", healthRows[0])
	}

	html := RenderHTML(Report{
		Kind:          KindOfficial,
		LocalDate:     "2026-07-30",
		Timezone:      "UTC",
		ClassChanges:  classRows,
		HealthChanges: healthRows,
	})
	if !contains(html, "Friendly News") {
		t.Fatal("html should show friendly name, not only id")
	}
	if contains(html, "Fleet health") {
		t.Fatal("should say System health")
	}
	text := RenderText(Report{ClassChanges: classRows, HealthChanges: healthRows})
	if !contains(text, "Friendly News") || contains(text, "Fleet health") {
		t.Fatalf("text=%q", text)
	}
}

func openOpsAttrStore(t *testing.T) *channelattr.Store {
	t.Helper()
	store, err := channelattr.Open(filepath.Join(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func mustPresence(t *testing.T, store *channelattr.Store, provider, id, name, state string, at time.Time) {
	t.Helper()
	val, err := json.Marshal(channelattr.Presence{State: state, Name: name, TvgID: id})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Handle(context.Background(), channelattr.Event{
		Provider: model.ProviderID(provider), ChannelID: id, Kind: channelattr.KindPresence,
		Value: val, At: at, Source: "test",
	}); err != nil {
		t.Fatal(err)
	}
}
