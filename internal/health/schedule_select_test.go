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

func TestL1ShouldSchedule(t *testing.T) {
	native := model.Channel{
		Classification: model.ClassNative,
		StreamURL:      "https://cdn.example/n.m3u8",
	}
	if !l1ShouldSchedule(native) {
		t.Fatal("NATIVE should schedule")
	}
	amagiProxy := model.Channel{
		Classification: model.ClassAmagiSSAI,
		StreamURL:      "https://amagi.example/beacon.m3u8",
		EmittedURL:     "https://proxy.example/stream/lg/a.m3u8",
	}
	if !l1ShouldSchedule(amagiProxy) {
		t.Fatal("Amagi with EmittedURL should schedule")
	}
	amagiNoProxy := model.Channel{
		Classification: model.ClassAmagiSSAI,
		StreamURL:      "https://amagi.example/beacon.m3u8",
	}
	if l1ShouldSchedule(amagiNoProxy) {
		t.Fatal("Amagi without EmittedURL must not schedule (would hit upstream beacon)")
	}
	session := model.Channel{
		Classification: model.ClassSession,
		StreamURL:      "https://dai.google.com/linear/hls/event/e/master.m3u8",
		EmittedURL:     "https://proxy.example/stream/distrotv/e.m3u8",
	}
	if l1ShouldSchedule(session) {
		t.Fatal("SESSION must not schedule L1")
	}
	stirrProxy := model.Channel{
		Classification: model.ClassStirrResolve,
		StreamURL:      "stirr://channel/5407",
		EmittedURL:     "https://proxy.example/stream/stirr/5407.m3u8",
	}
	if !l1ShouldSchedule(stirrProxy) {
		t.Fatal("STIRR with EmittedURL should schedule (probe via proxy)")
	}
	stirrOpaque := model.Channel{
		Classification: model.ClassStirrResolve,
		StreamURL:      "stirr://channel/5407",
	}
	if l1ShouldSchedule(stirrOpaque) {
		t.Fatal("STIRR without EmittedURL must not schedule")
	}
	distroProxy := model.Channel{
		Classification: model.ClassDistroResolve,
		StreamURL:      "distro://jsrdn/feed/abc",
		EmittedURL:     "https://proxy.example/stream/distrotv/abc.m3u8",
	}
	if !l1ShouldSchedule(distroProxy) {
		t.Fatal("Distro with EmittedURL should schedule")
	}
	excluded := native
	excluded.Excluded = true
	if l1ShouldSchedule(excluded) {
		t.Fatal("excluded must not schedule")
	}
}

func TestL1RetryCandidate(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	native := model.Channel{
		Classification: model.ClassNative,
		StreamURL:      "https://cdn.example/n.m3u8",
	}
	due := model.ChannelHealth{Status: model.HealthDegraded, NextRetryAt: now}
	if !l1RetryCandidate(native, due, now) {
		t.Fatal("degraded NATIVE due should retry")
	}
	if l1RetryCandidate(native, model.ChannelHealth{Status: model.HealthHealthy, NextRetryAt: now}, now) {
		t.Fatal("healthy must not retry")
	}
	if l1RetryCandidate(native, model.ChannelHealth{Status: model.HealthDegraded}, now) {
		t.Fatal("no next_retry_at must not retry")
	}
	session := model.Channel{
		Classification: model.ClassSession,
		StreamURL:      "https://dai.google.com/linear/hls/event/e/master.m3u8",
		EmittedURL:     "https://proxy.example/stream/distrotv/e.m3u8",
	}
	if l1RetryCandidate(session, due, now) {
		t.Fatal("SESSION must not enter retry lane")
	}
	amagiNoProxy := model.Channel{
		Classification: model.ClassAmagiSSAI,
		StreamURL:      "https://amagi.example/beacon.m3u8",
	}
	if l1RetryCandidate(amagiNoProxy, due, now) {
		t.Fatal("Amagi without EmittedURL must not retry")
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
