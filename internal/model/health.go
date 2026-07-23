package model

import "time"

// Health is the current playability label for a channel (FSM output).
type Health string

const (
	HealthUntested Health = "untested" // never observed
	HealthHealthy  Health = "healthy"  // last streak is clean
	HealthDegraded Health = "degraded" // recent failures, streak < N
	HealthDown     Health = "down"     // streak >= N consecutive failures
)

// HealthCheckResult is the result of a single probe/playback attempt.
type HealthCheckResult string

const (
	HealthCheckSuccess HealthCheckResult = "success"
	HealthCheckFailure HealthCheckResult = "failure"
)

// HealthCheck is one try of the stream (probe or playback). Feeds Apply; the
// attr store persists ChannelHealth (current + history), not raw checks alone.
type HealthCheck struct {
	Result       HealthCheckResult
	FailureClass string // empty on success
	At           time.Time
	Source       string // "probe", "playback", …
}

// ChannelHealth is the current (and history-row) value for kind=health.
type ChannelHealth struct {
	Status              Health            `json:"status"`
	ConsecutiveFailures int               `json:"consecutive_failures"`
	LastCheckAt         time.Time         `json:"last_check_at,omitempty"`
	LastCheck           HealthCheckResult `json:"last_check,omitempty"`
	LastFailureClass    string            `json:"last_failure_class,omitempty"`
}

// Apply returns the next ChannelHealth after one check. n is the DOWN threshold
// (minimum 1). Zero-value receiver is treated as untested.
func (h ChannelHealth) Apply(check HealthCheck, n int) ChannelHealth {
	if n < 1 {
		n = 1
	}
	at := check.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	out := ChannelHealth{
		LastCheckAt: at,
		LastCheck:   check.Result,
	}
	switch check.Result {
	case HealthCheckSuccess:
		out.Status = HealthHealthy
		out.ConsecutiveFailures = 0
		return out
	case HealthCheckFailure:
		out.ConsecutiveFailures = h.ConsecutiveFailures + 1
		out.LastFailureClass = check.FailureClass
		if out.ConsecutiveFailures >= n {
			out.Status = HealthDown
		} else {
			out.Status = HealthDegraded
		}
		return out
	default:
		// Unknown result: leave prior status, still record the check time/result.
		out.Status = h.StatusOrUntested()
		out.ConsecutiveFailures = h.ConsecutiveFailures
		out.LastFailureClass = h.LastFailureClass
		return out
	}
}

// StatusOrUntested returns Status, or HealthUntested when empty.
func (h ChannelHealth) StatusOrUntested() Health {
	if h.Status == "" {
		return HealthUntested
	}
	return h.Status
}
