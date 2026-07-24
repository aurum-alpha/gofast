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
	Result       HealthCheckResult `json:"result"`
	FailureClass string            `json:"failure_class,omitempty"`
	// Detail is the human-readable cause (HTTP status line, net error, ffprobe
	// stderr). Kept short; omitted on success.
	Detail string `json:"detail,omitempty"`
	// HTTPStatus is the final HTTP status from an L2 (segment) probe when the
	// check involved an HTTP response (success or failure). Zero if N/A.
	HTTPStatus int       `json:"http_status,omitempty"`
	At         time.Time `json:"at"`
	Source     string    `json:"source,omitempty"`
}

// ChannelHealth is the current (and history-row) value for kind=health.
type ChannelHealth struct {
	Status              Health            `json:"status"`
	ConsecutiveFailures int               `json:"consecutive_failures"`
	LastCheckAt         time.Time         `json:"last_check_at,omitempty"`
	LastCheck           HealthCheckResult `json:"last_check,omitempty"`
	LastFailureClass    string            `json:"last_failure_class,omitempty"`
	LastFailureDetail   string            `json:"last_failure_detail,omitempty"`
	// LastHTTPStatus is from the latest check that recorded one (L2 HTTP).
	LastHTTPStatus int `json:"last_http_status,omitempty"`
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
		LastCheckAt:    at,
		LastCheck:      check.Result,
		LastHTTPStatus: check.HTTPStatus,
	}
	switch check.Result {
	case HealthCheckSuccess:
		out.Status = HealthHealthy
		out.ConsecutiveFailures = 0
		return out
	case HealthCheckFailure:
		out.ConsecutiveFailures = h.ConsecutiveFailures + 1
		out.LastFailureClass = check.FailureClass
		out.LastFailureDetail = check.Detail
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
		out.LastFailureDetail = h.LastFailureDetail
		if out.LastHTTPStatus == 0 {
			out.LastHTTPStatus = h.LastHTTPStatus
		}
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
