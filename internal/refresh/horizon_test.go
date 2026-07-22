package refresh

import (
	"testing"
	"time"
)

func TestClampInterval(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		configured time.Duration
		horizon    time.Duration
		want       time.Duration
		clamped    bool
	}{
		{
			name:       "configured within half horizon",
			configured: 6 * time.Hour,
			horizon:    12 * time.Hour,
			want:       6 * time.Hour,
			clamped:    false,
		},
		{
			name:       "configured above half horizon",
			configured: 10 * time.Hour,
			horizon:    12 * time.Hour,
			want:       6 * time.Hour,
			clamped:    true,
		},
		{
			name:       "empirical short horizon",
			configured: 6 * time.Hour,
			horizon:    4 * time.Hour,
			want:       2 * time.Hour,
			clamped:    true,
		},
		{
			name:       "unknown horizon keeps configured",
			configured: 6 * time.Hour,
			horizon:    0,
			want:       6 * time.Hour,
			clamped:    false,
		},
		{
			name:       "tiny configured falls back then clamps",
			configured: 30 * time.Second,
			horizon:    12 * time.Hour,
			want:       6 * time.Hour,
			clamped:    false,
		},
		{
			name:       "horizon floor at minInterval",
			configured: time.Hour,
			horizon:    time.Minute,
			want:       time.Minute,
			clamped:    true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, clamped := ClampInterval(tc.configured, tc.horizon)
			if got != tc.want || clamped != tc.clamped {
				t.Fatalf("ClampInterval(%v, %v) = (%v, %v); want (%v, %v)",
					tc.configured, tc.horizon, got, clamped, tc.want, tc.clamped)
			}
		})
	}
}

func TestResolveHorizon(t *testing.T) {
	t.Parallel()
	if got := resolveHorizon(4*time.Hour, 12*time.Hour); got != 4*time.Hour {
		t.Fatalf("prefer empirical: got %v", got)
	}
	if got := resolveHorizon(0, 12*time.Hour); got != 12*time.Hour {
		t.Fatalf("fall back to expected: got %v", got)
	}
}
