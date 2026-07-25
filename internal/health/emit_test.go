package health

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/channelattr"
	"github.com/j27-aurum/gofast/internal/model"
)

func TestEmitCheckAppliesAndPersists(t *testing.T) {
	store, err := channelattr.Open(filepath.Join(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	bus := channelattr.NewBus(8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go channelattr.Receive(ctx, bus, store)

	em := &Emitter{Bus: bus, Store: store, ConsecutiveFailures: 3}
	at := time.Date(2026, 7, 22, 15, 0, 0, 0, time.UTC)
	if _, err := em.EmitCheck(ctx, model.ProviderLG, "ch-a", model.HealthCheck{
		Result:       model.HealthCheckFailure,
		FailureClass: "http_403",
		At:           at,
		Source:       "probe",
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var got model.ChannelHealth
	for time.Now().Before(deadline) {
		raw, ok := store.Current(model.ProviderLG, "ch-a", channelattr.KindHealth)
		if !ok {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if got.Status != model.HealthDegraded || got.ConsecutiveFailures != 1 {
			t.Fatalf("got %+v", got)
		}
		return
	}
	t.Fatal("timed out")
}

func TestEmitCheckChainsBeforeStoreCatchUp(t *testing.T) {
	store, err := channelattr.Open(filepath.Join(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	bus := channelattr.NewBus(8)
	// Do not start Receive — Store.Current stays empty so chaining must use live prior.
	em := &Emitter{Bus: bus, Store: store, ConsecutiveFailures: 3}
	ctx := context.Background()
	at := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	var next model.ChannelHealth
	for i := 0; i < 3; i++ {
		next, err = em.EmitCheck(ctx, model.ProviderLG, "ch-b", model.HealthCheck{
			Result: model.HealthCheckFailure, FailureClass: "x", At: at.Add(time.Duration(i) * time.Second),
			Source: SourcePlayback,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if next.Status != model.HealthDown || next.ConsecutiveFailures != 3 {
		t.Fatalf("got %+v", next)
	}
}
