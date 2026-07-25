package model

import (
	"testing"
	"time"
)

func TestChannelHealthApply(t *testing.T) {
	at := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	ok := HealthCheck{Result: HealthCheckSuccess, At: at, Source: "probe"}
	fail := func(class, detail string) HealthCheck {
		return HealthCheck{Result: HealthCheckFailure, FailureClass: class, Detail: detail, At: at, Source: "probe"}
	}

	tests := []struct {
		name  string
		prev  ChannelHealth
		check HealthCheck
		n     int
		want  ChannelHealth
	}{
		{
			name:  "zero stays conceptually untested until a check",
			prev:  ChannelHealth{},
			check: HealthCheck{}, // unknown result
			n:     3,
			want: ChannelHealth{
				Status:              HealthUntested,
				ConsecutiveFailures: 0,
				LastCheckAt:         at, // Apply fills zero At — we'll set check.At below
			},
		},
		{
			name:  "success from zero → healthy",
			prev:  ChannelHealth{},
			check: ok,
			n:     3,
			want: ChannelHealth{
				Status:              HealthHealthy,
				ConsecutiveFailures: 0,
				LastCheckAt:         at,
				LastCheck:           HealthCheckSuccess,
			},
		},
		{
			name:  "success records http status",
			prev:  ChannelHealth{},
			check: HealthCheck{Result: HealthCheckSuccess, At: at, Source: "health_l1", HTTPStatus: 206},
			n:     3,
			want: ChannelHealth{
				Status:              HealthHealthy,
				ConsecutiveFailures: 0,
				LastCheckAt:         at,
				LastCheck:           HealthCheckSuccess,
				LastHTTPStatus:      206,
			},
		},
		{
			name:  "failure records http status",
			prev:  ChannelHealth{Status: HealthHealthy},
			check: HealthCheck{Result: HealthCheckFailure, FailureClass: "http_416", Detail: "playlist HTTP 416", At: at, HTTPStatus: 416},
			n:     3,
			want: ChannelHealth{
				Status:              HealthDegraded,
				ConsecutiveFailures: 1,
				LastCheckAt:         at,
				LastCheck:           HealthCheckFailure,
				LastFailureClass:    "http_416",
				LastFailureDetail:   "playlist HTTP 416",
				LastHTTPStatus:      416,
				NextRetryAt:         at.Add(15 * time.Minute),
				RetryStep:           1,
			},
		},
		{
			name:  "failure from healthy → degraded streak 1",
			prev:  ChannelHealth{Status: HealthHealthy},
			check: fail("http_403", "playlist HTTP 403 Forbidden"),
			n:     3,
			want: ChannelHealth{
				Status:              HealthDegraded,
				ConsecutiveFailures: 1,
				LastCheckAt:         at,
				LastCheck:           HealthCheckFailure,
				LastFailureClass:    "http_403",
				LastFailureDetail:   "playlist HTTP 403 Forbidden",
				NextRetryAt:         at.Add(15 * time.Minute),
				RetryStep:           1,
			},
		},
		{
			name: "degraded streak 2 + failure → down",
			prev: ChannelHealth{
				Status:              HealthDegraded,
				ConsecutiveFailures: 2,
				RetryStep:           1,
				NextRetryAt:         at.Add(-time.Minute),
			},
			check: fail("empty_segment", "body_len=12"),
			n:     3,
			want: ChannelHealth{
				Status:              HealthDown,
				ConsecutiveFailures: 3,
				LastCheckAt:         at,
				LastCheck:           HealthCheckFailure,
				LastFailureClass:    "empty_segment",
				LastFailureDetail:   "body_len=12",
				NextRetryAt:         at.Add(30 * time.Minute),
				RetryStep:           2,
			},
		},
		{
			name: "down + success → healthy clears failure detail and retry",
			prev: ChannelHealth{
				Status:              HealthDown,
				ConsecutiveFailures: 4,
				LastFailureClass:    "ffprobe",
				LastFailureDetail:   "old detail",
				NextRetryAt:         at.Add(time.Hour),
				RetryStep:           3,
			},
			check: ok,
			n:     3,
			want: ChannelHealth{
				Status:              HealthHealthy,
				ConsecutiveFailures: 0,
				LastCheckAt:         at,
				LastCheck:           HealthCheckSuccess,
			},
		},
		{
			name: "down + failure keeps down, streak grows",
			prev: ChannelHealth{
				Status:              HealthDown,
				ConsecutiveFailures: 3,
				LastFailureClass:    "old",
				LastFailureDetail:   "old detail",
				RetryStep:           2,
			},
			check: fail("new", "new detail"),
			n:     3,
			want: ChannelHealth{
				Status:              HealthDown,
				ConsecutiveFailures: 4,
				LastCheckAt:         at,
				LastCheck:           HealthCheckFailure,
				LastFailureClass:    "new",
				LastFailureDetail:   "new detail",
				NextRetryAt:         at.Add(time.Hour),
				RetryStep:           3,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			check := tc.check
			if check.At.IsZero() && check.Result == "" {
				check.At = at
			}
			got := tc.prev.Apply(check, tc.n)
			if check.Result == "" {
				// unknown path: LastCheck empty, StatusOrUntested
				if got.Status != HealthUntested || got.ConsecutiveFailures != 0 {
					t.Fatalf("got %+v", got)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
		})
	}
}

func TestChannelHealthApplyL1RetryBackoff(t *testing.T) {
	at := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	fail := HealthCheck{Result: HealthCheckFailure, FailureClass: "http_5xx", Detail: "502", At: at}

	// Walk the full table then park.
	wantDelays := []time.Duration{
		15 * time.Minute,
		30 * time.Minute,
		time.Hour,
		2 * time.Hour,
		6 * time.Hour,
	}
	h := ChannelHealth{Status: HealthHealthy}
	for i, delay := range wantDelays {
		h = h.Apply(fail, 3)
		if h.Status != HealthDegraded && h.Status != HealthDown {
			t.Fatalf("step %d status %q", i, h.Status)
		}
		if !h.NextRetryAt.Equal(at.Add(delay)) {
			t.Fatalf("step %d NextRetryAt=%v want %v", i, h.NextRetryAt, at.Add(delay))
		}
		if h.RetryStep != i+1 {
			t.Fatalf("step %d RetryStep=%d want %d", i, h.RetryStep, i+1)
		}
	}
	// Failure after last armed step parks.
	h = h.Apply(fail, 3)
	if !h.NextRetryAt.IsZero() || h.RetryStep != 0 {
		t.Fatalf("park: NextRetryAt=%v RetryStep=%d", h.NextRetryAt, h.RetryStep)
	}
	// Baseline (or any) failure re-arms from step 0.
	h = h.Apply(fail, 3)
	if !h.NextRetryAt.Equal(at.Add(15*time.Minute)) || h.RetryStep != 1 {
		t.Fatalf("re-arm: NextRetryAt=%v RetryStep=%d", h.NextRetryAt, h.RetryStep)
	}
}

func TestChannelHealthRetryDue(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	if (ChannelHealth{Status: HealthDegraded, NextRetryAt: now.Add(time.Minute)}).RetryDue(now) {
		t.Fatal("future due should be false")
	}
	if !(ChannelHealth{Status: HealthDown, NextRetryAt: now}).RetryDue(now) {
		t.Fatal("exact due should be true")
	}
	if (ChannelHealth{Status: HealthHealthy, NextRetryAt: now}).RetryDue(now) {
		t.Fatal("healthy should not be due")
	}
	if (ChannelHealth{Status: HealthDegraded}).RetryDue(now) {
		t.Fatal("zero NextRetryAt should not be due")
	}
}

func TestChannelHealthStatusOrUntested(t *testing.T) {
	if (ChannelHealth{}).StatusOrUntested() != HealthUntested {
		t.Fatal("empty status should be untested")
	}
	if (ChannelHealth{Status: HealthDown}).StatusOrUntested() != HealthDown {
		t.Fatal("explicit status should pass through")
	}
}
