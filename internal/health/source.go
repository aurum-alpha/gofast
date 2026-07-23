package health

import (
	"context"

	"github.com/j27-aurum/gofast/internal/model"
)

// Source feeds the health FSM. Probes implement this; playback telemetry (M5) will too.
type Source interface {
	// Check returns one HealthCheck for the channel (success or failure).
	Check(ctx context.Context, ch model.Channel) model.HealthCheck
}
