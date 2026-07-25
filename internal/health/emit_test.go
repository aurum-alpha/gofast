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
		if !got.NextRetryAt.Equal(at.Add(15*time.Minute)) || got.RetryStep != 1 {
			t.Fatalf("retry arm: %+v", got)
		}
		return
	}
	t.Fatal("timed out")
}

func TestEmitCheckSuccessClearsRetry(t *testing.T) {
	bus := channelattr.NewBus(8)
	em := &Emitter{Bus: bus, ConsecutiveFailures: 3}
	ctx := context.Background()
	at := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	fail, err := em.EmitCheck(ctx, model.ProviderLG, "ch-retry", model.HealthCheck{
		Result: model.HealthCheckFailure, FailureClass: "x", At: at, Source: "health_l1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fail.NextRetryAt.IsZero() {
		t.Fatal("expected next_retry_at after failure")
	}
	ok, err := em.EmitCheck(ctx, model.ProviderLG, "ch-retry", model.HealthCheck{
		Result: model.HealthCheckSuccess, At: at.Add(time.Minute), Source: "health_l1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ok.NextRetryAt.IsZero() || ok.RetryStep != 0 || ok.Status != model.HealthHealthy {
		t.Fatalf("success should clear retry: %+v", ok)
	}
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
