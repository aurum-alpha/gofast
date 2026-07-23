package model

import (
	"testing"
	"time"
)

func TestChannelHealthApply(t *testing.T) {
	at := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	ok := HealthCheck{Result: HealthCheckSuccess, At: at, Source: "probe"}
	fail := func(class string) HealthCheck {
		return HealthCheck{Result: HealthCheckFailure, FailureClass: class, At: at, Source: "probe"}
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
			name:  "failure from healthy → degraded streak 1",
			prev:  ChannelHealth{Status: HealthHealthy},
			check: fail("http_403"),
			n:     3,
			want: ChannelHealth{
				Status:              HealthDegraded,
				ConsecutiveFailures: 1,
				LastCheckAt:         at,
				LastCheck:           HealthCheckFailure,
				LastFailureClass:    "http_403",
			},
		},
		{
			name: "degraded streak 2 + failure → down",
			prev: ChannelHealth{
				Status:              HealthDegraded,
				ConsecutiveFailures: 2,
			},
			check: fail("empty_segment"),
			n:     3,
			want: ChannelHealth{
				Status:              HealthDown,
				ConsecutiveFailures: 3,
				LastCheckAt:         at,
				LastCheck:           HealthCheckFailure,
				LastFailureClass:    "empty_segment",
			},
		},
		{
			name: "down + success → healthy",
			prev: ChannelHealth{
				Status:              HealthDown,
				ConsecutiveFailures: 4,
				LastFailureClass:    "ffprobe",
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
			},
			check: fail("new"),
			n:     3,
			want: ChannelHealth{
				Status:              HealthDown,
				ConsecutiveFailures: 4,
				LastCheckAt:         at,
				LastCheck:           HealthCheckFailure,
				LastFailureClass:    "new",
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

func TestChannelHealthStatusOrUntested(t *testing.T) {
	if (ChannelHealth{}).StatusOrUntested() != HealthUntested {
		t.Fatal("empty status should be untested")
	}
	if (ChannelHealth{Status: HealthDown}).StatusOrUntested() != HealthDown {
		t.Fatal("explicit status should pass through")
	}
}
