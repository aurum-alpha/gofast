package health

import (
	"time"

	"github.com/j27-aurum/gofast/internal/model"
)

// SourcePlayback is the channelattr Event.Source for proxy-driven health checks.
const SourcePlayback = "playback"

// SourceL1Retry is HealthCheck.Source for the L1 retry lane (J27-67).
const SourceL1Retry = "health_l1_retry"

// HealthCheckFromProxyEvent maps a FASTProxy telemetry event into a HealthCheck
// for EmitCheck. ok is false when the event should not drive the FSM (too chatty
// or not an upstream health signal).
func HealthCheckFromProxyEvent(kind, reason, message string, status int, durationMS int64, at time.Time) (model.HealthCheck, bool) {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	base := model.HealthCheck{
		At:         at,
		Source:     SourcePlayback,
		HTTPStatus: status,
		DurationMs: durationMS,
		Detail:     message,
	}
	switch kind {
	case "playlist_ok":
		base.Result = model.HealthCheckSuccess
		return base, true
	case "playlist_fail", "origin_miss":
		base.Result = model.HealthCheckFailure
		base.FailureClass = reason
		if base.FailureClass == "" {
			base.FailureClass = kind
		}
		return base, true
	case "seg_fail":
		if reason == "client_cancel" {
			return model.HealthCheck{}, false
		}
		base.Result = model.HealthCheckFailure
		base.FailureClass = reason
		if base.FailureClass == "" {
			base.FailureClass = kind
		}
		return base, true
	default:
		return model.HealthCheck{}, false
	}
}
