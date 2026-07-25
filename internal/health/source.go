package health

import (
	"context"

	"github.com/j27-aurum/gofast/internal/model"
)

// Source feeds the health FSM via pull (scheduler / on-demand Probe).
// Probes implement Check. Playback telemetry is push: proxy events are mapped
// with HealthCheckFromProxyEvent and passed to Emitter.EmitCheck (same seam,
// different intake).
type Source interface {
	// Check returns one HealthCheck for the channel (success or failure).
	Check(ctx context.Context, ch model.Channel) model.HealthCheck
}
