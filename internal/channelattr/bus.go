package channelattr

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Bus is a process-wide emit channel. Producers only send; they never open SQLite.
type Bus chan Event

// NewBus returns a buffered attribute event bus.
func NewBus(buffer int) Bus {
	if buffer < 1 {
		buffer = 256
	}
	return make(Bus, buffer)
}

// Emit sends ev on the bus, blocking for backpressure until ctx is done.
func Emit(ctx context.Context, bus Bus, ev Event) error {
	if bus == nil {
		return fmt.Errorf("channelattr: nil bus")
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case bus <- ev:
		return nil
	}
}

// Receive runs until ctx is cancelled. It is the sole writer to store disk + memory.
func Receive(ctx context.Context, bus Bus, store *Store) {
	if bus == nil || store == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-bus:
			if !ok {
				return
			}
			if err := store.Handle(ctx, ev); err != nil {
				slog.Warn("channelattr receive",
					"err", err,
					"provider", ev.Provider,
					"channel", ev.ChannelID,
					"kind", ev.Kind,
				)
			}
		}
	}
}
