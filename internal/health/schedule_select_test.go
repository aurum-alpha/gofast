package health

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
)

func TestL3ShouldProbe(t *testing.T) {
	cases := []struct {
		status model.Health
		sample float64
		roll   float64
		want   bool
	}{
		{model.HealthDegraded, 0.1, 0.99, true},
		{model.HealthDown, 0.1, 0.99, true},
		{model.HealthUntested, 0.1, 0.99, true},
		{"", 0.1, 0.99, true},
		{model.HealthHealthy, 0.1, 0.05, true},
		{model.HealthHealthy, 0.1, 0.5, false},
		{model.HealthHealthy, 1.0, 0.99, true},
		{model.HealthHealthy, 0.0, 0.0, false},
	}
	for _, tc := range cases {
		if got := l3ShouldProbe(tc.status, tc.sample, tc.roll); got != tc.want {
			t.Fatalf("status=%s sample=%v roll=%v got %v want %v", tc.status, tc.sample, tc.roll, got, tc.want)
		}
	}
}

func TestHostLimiterCapsPerHost(t *testing.T) {
	lim := NewHostLimiter(1)
	ctx := context.Background()
	release, err := lim.Acquire(ctx, "https://cdn.example/a")
	if err != nil {
		t.Fatal(err)
	}

	blocked := make(chan struct{})
	var gotThird atomic.Bool
	go func() {
		close(blocked)
		r, err := lim.Acquire(ctx, "https://cdn.example/b")
		if err != nil {
			t.Error(err)
			return
		}
		gotThird.Store(true)
		r()
	}()
	<-blocked
	time.Sleep(30 * time.Millisecond)
	if gotThird.Load() {
		t.Fatal("second acquire should wait")
	}
	release()
	deadline := time.Now().Add(time.Second)
	for !gotThird.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !gotThird.Load() {
		t.Fatal("second acquire should proceed after release")
	}
}

func TestFFProbeHeadersFormat(t *testing.T) {
	got := ffprobeHeaders(map[string]string{"User-Agent": "GoFAST", "X-Test": "1"})
	if !strings.Contains(got, "User-Agent: GoFAST") || !strings.Contains(got, "X-Test: 1") {
		t.Fatalf("headers = %q", got)
	}
}
