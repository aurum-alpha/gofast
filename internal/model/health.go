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
	HTTPStatus int `json:"http_status,omitempty"`
	// DurationMs is wall time for the check (all attempts including soft retry).
	DurationMs int64 `json:"duration_ms,omitempty"`
	// FinalURL is the URL after redirects (L2 segment or probe target).
	FinalURL string `json:"final_url,omitempty"`
	// BytesRead is response body bytes for the decisive L2 GET.
	BytesRead int `json:"bytes_read,omitempty"`
	// RangeUsed is true when the decisive request used a Range header.
	RangeUsed bool `json:"range_used,omitempty"`
	// RangeRetried is true when a 416 caused a retry without Range.
	RangeRetried bool      `json:"range_retried,omitempty"`
	At           time.Time `json:"at"`
	Source       string    `json:"source,omitempty"`
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
	// Probe metadata from the latest check (mirrored from HealthCheck).
	LastDurationMs   int64  `json:"last_duration_ms,omitempty"`
	LastFinalURL     string `json:"last_final_url,omitempty"`
	LastBytesRead    int    `json:"last_bytes_read,omitempty"`
	LastRangeUsed    bool   `json:"last_range_used,omitempty"`
	LastRangeRetried bool   `json:"last_range_retried,omitempty"`
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
		LastCheckAt:      at,
		LastCheck:        check.Result,
		LastHTTPStatus:   check.HTTPStatus,
		LastDurationMs:   check.DurationMs,
		LastFinalURL:     check.FinalURL,
		LastBytesRead:    check.BytesRead,
		LastRangeUsed:    check.RangeUsed,
		LastRangeRetried: check.RangeRetried,
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
		if out.LastDurationMs == 0 {
			out.LastDurationMs = h.LastDurationMs
		}
		if out.LastFinalURL == "" {
			out.LastFinalURL = h.LastFinalURL
		}
		if out.LastBytesRead == 0 {
			out.LastBytesRead = h.LastBytesRead
		}
		if !out.LastRangeUsed {
			out.LastRangeUsed = h.LastRangeUsed
		}
		if !out.LastRangeRetried {
			out.LastRangeRetried = h.LastRangeRetried
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
