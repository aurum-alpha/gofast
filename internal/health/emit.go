package health

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/j27-aurum/gofast/internal/channelattr"
	"github.com/j27-aurum/gofast/internal/model"
)

// Emitter applies a check to the prior ChannelHealth and Emits kind=health.
// ConsecutiveFailures may be set in the literal before use; after probes start,
// change it only via SetConsecutiveFailures (config hot reload).
type Emitter struct {
	Bus                 channelattr.Bus
	Store               *channelattr.Store // for reading prior current; may be nil
	ConsecutiveFailures int

	mu sync.Mutex // guards ConsecutiveFailures after probes start
}

// EmitCheck folds check into current health and sends the result on the bus.
// The returned ChannelHealth is the value that will be persisted (bus is async).
func (e *Emitter) EmitCheck(ctx context.Context, provider model.ProviderID, channelID string, check model.HealthCheck) (model.ChannelHealth, error) {
	if e == nil || e.Bus == nil {
		return model.ChannelHealth{}, fmt.Errorf("health: nil emitter or bus")
	}
	n := e.failN()
	prev := model.ChannelHealth{}
	if e.Store != nil {
		if raw, ok := e.Store.Current(provider, channelID, channelattr.KindHealth); ok {
			_ = json.Unmarshal(raw, &prev)
		}
	}
	if check.At.IsZero() {
		check.At = time.Now().UTC()
	}
	next := prev.Apply(check, n)
	value, err := json.Marshal(next)
	if err != nil {
		return model.ChannelHealth{}, err
	}
	src := check.Source
	if src == "" {
		src = "probe"
	}
	if err := channelattr.Emit(ctx, e.Bus, channelattr.Event{
		Provider:  provider,
		ChannelID: channelID,
		Kind:      channelattr.KindHealth,
		Value:     value,
		At:        check.At,
		Source:    src,
	}); err != nil {
		return model.ChannelHealth{}, err
	}
	return next, nil
}

// SetConsecutiveFailures updates N for DOWN at runtime (config hot reload).
func (e *Emitter) SetConsecutiveFailures(n int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ConsecutiveFailures = n
}

// failN returns the effective N for DOWN (default 3).
func (e *Emitter) failN() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ConsecutiveFailures < 1 {
		return 3
	}
	return e.ConsecutiveFailures
}
