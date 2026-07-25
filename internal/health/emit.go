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

	mu   sync.Mutex
	live map[string]model.ChannelHealth // process-local prior (Store writes are async)
}

// Current returns the latest ChannelHealth for a channel from the process-local
// prior, falling back to the attr store. ok is false when neither has a value.
func (e *Emitter) Current(provider model.ProviderID, channelID string) (model.ChannelHealth, bool) {
	if e == nil {
		return model.ChannelHealth{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	key := string(provider) + "\x00" + channelID
	if h, ok := e.live[key]; ok {
		return h, true
	}
	if e.Store != nil {
		if raw, found := e.Store.Current(provider, channelID, channelattr.KindHealth); found {
			var h model.ChannelHealth
			if json.Unmarshal(raw, &h) == nil {
				return h, true
			}
		}
	}
	return model.ChannelHealth{}, false
}

// EmitCheck folds check into current health and sends the result on the bus.
// The returned ChannelHealth is the value that will be persisted (bus is async).
// A process-local prior is kept so rapid/batched checks chain correctly before
// the attr receiver persists Current.
func (e *Emitter) EmitCheck(ctx context.Context, provider model.ProviderID, channelID string, check model.HealthCheck) (model.ChannelHealth, error) {
	if e == nil || e.Bus == nil {
		return model.ChannelHealth{}, fmt.Errorf("health: nil emitter or bus")
	}
	if check.At.IsZero() {
		check.At = time.Now().UTC()
	}

	e.mu.Lock()
	n := e.failNLocked()
	key := string(provider) + "\x00" + channelID
	prev, ok := e.live[key]
	if !ok && e.Store != nil {
		if raw, found := e.Store.Current(provider, channelID, channelattr.KindHealth); found {
			_ = json.Unmarshal(raw, &prev)
		}
	}
	next := prev.Apply(check, n)
	if e.live == nil {
		e.live = make(map[string]model.ChannelHealth)
	}
	e.live[key] = next
	e.mu.Unlock()

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
	return e.failNLocked()
}

func (e *Emitter) failNLocked() int {
	if e.ConsecutiveFailures < 1 {
		return 3
	}
	return e.ConsecutiveFailures
}
