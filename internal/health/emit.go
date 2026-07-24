package health

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/j27-aurum/gofast/internal/channelattr"
	"github.com/j27-aurum/gofast/internal/model"
)

// Emitter applies a check to the prior ChannelHealth and Emits kind=health.
type Emitter struct {
	Bus                 channelattr.Bus
	Store               *channelattr.Store // for reading prior current; may be nil
	ConsecutiveFailures int
}

// EmitCheck folds check into current health and sends the result on the bus.
// The returned ChannelHealth is the value that will be persisted (bus is async).
func (e *Emitter) EmitCheck(ctx context.Context, provider model.ProviderID, channelID string, check model.HealthCheck) (model.ChannelHealth, error) {
	if e == nil || e.Bus == nil {
		return model.ChannelHealth{}, fmt.Errorf("health: nil emitter or bus")
	}
	n := e.ConsecutiveFailures
	if n < 1 {
		n = 3
	}
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
