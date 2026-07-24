package health

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/j27-aurum/gofast/internal/channelattr"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

var ErrNoProber = errors.New("health: L3 prober not configured")
var ErrNoSegmentProber = errors.New("health: L2 segment prober not configured")

// Scheduler runs L2 (NATIVE segment) and optional L3 (ffprobe) loops.
// Timing is anchored on the last completed sweep (persisted under StatePath),
// not process start — same idea as provider refresh from FetchedAt.
type Scheduler struct {
	Reg       *provider.Registry
	Emitter   *Emitter
	Segment   *SegmentProber
	FFProbe   *FFProbe
	L2Every   time.Duration
	L3Every   time.Duration // interval between L3 sweeps when L3On
	L3On      bool
	L2Workers int // global L2 concurrency (default 4)
	L3Workers int
	// L3HealthySample is fraction of healthy channels probed each L3 sweep (0–1; default 0.1).
	L3HealthySample float64
	Hosts           *HostLimiter
	// StatePath is optional JSON for last/next L2/L3 (e.g. {data_dir}/channelattr/health_schedule.json).
	StatePath string

	mu       sync.Mutex
	lastL2At time.Time
	nextL2At time.Time
	lastL3At time.Time
	nextL3At time.Time
	l2Busy   bool
	l3Busy   bool
}

// Schedule is a read-only snapshot for APIs / UI.
type Schedule struct {
	L2Interval string    `json:"l2_interval"`
	LastL2At   time.Time `json:"last_l2_at,omitempty"`
	NextL2At   time.Time `json:"next_l2_at,omitempty"`
	L2Running  bool      `json:"l2_running"`
	L3Enabled  bool      `json:"l3_enabled"`
	L3Interval string    `json:"l3_interval,omitempty"`
	LastL3At   time.Time `json:"last_l3_at,omitempty"`
	NextL3At   time.Time `json:"next_l3_at,omitempty"`
	L3Running  bool      `json:"l3_running"`
}

// Snapshot returns the current L2/L3 schedule for display.
func (s *Scheduler) Snapshot() Schedule {
	if s == nil {
		return Schedule{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Schedule{
		L2Interval: s.l2Interval().String(),
		LastL2At:   s.lastL2At,
		NextL2At:   s.nextL2At,
		L2Running:  s.l2Busy,
		L3Enabled:  s.L3On,
		L3Running:  s.l3Busy,
	}
	if s.L3On {
		out.L3Interval = s.l3Interval().String()
		out.LastL3At = s.lastL3At
		out.NextL3At = s.nextL3At
	}
	return out
}

// Run blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	if s == nil || s.Reg == nil || s.Emitter == nil {
		return
	}
	s.loadState()

	now := time.Now()
	l2Delay := s.delayUntilNext(now, s.lastL2Locked(), s.l2Interval(), true)
	s.setNextL2(now.Add(l2Delay))
	s.saveState()
	l2Timer := time.NewTimer(l2Delay)
	defer l2Timer.Stop()
	slog.Info("health L2 schedule",
		"last_l2_at", nullTime(s.lastL2Locked()),
		"next_l2_at", now.Add(l2Delay).UTC(),
		"interval", s.l2Interval().String(),
	)

	var l3Timer *time.Timer
	if s.L3On {
		l3Delay := s.delayUntilNext(now, s.lastL3Locked(), s.l3Interval(), true)
		s.setNextL3(now.Add(l3Delay))
		s.saveState()
		l3Timer = time.NewTimer(l3Delay)
		defer l3Timer.Stop()
		slog.Info("health L3 schedule",
			"last_l3_at", nullTime(s.lastL3Locked()),
			"next_l3_at", now.Add(l3Delay).UTC(),
			"interval", s.l3Interval().String(),
			"healthy_sample", s.l3HealthySample(),
		)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-l2Timer.C:
			s.mu.Lock()
			s.l2Busy = true
			s.mu.Unlock()
			s.runL2(ctx)
			delay := jitter(s.l2Interval(), 0.1)
			done := time.Now()
			s.finishL2(done, done.Add(delay))
			s.saveState()
			l2Timer.Reset(delay)
		case <-l3Fire(l3Timer):
			if s.L3On && s.FFProbe != nil {
				s.mu.Lock()
				s.l3Busy = true
				s.mu.Unlock()
				s.runL3(ctx)
				delay := jitter(s.l3Interval(), 1.0)
				done := time.Now()
				s.finishL3(done, done.Add(delay))
				s.saveState()
				l3Timer.Reset(delay)
			}
		}
	}
}

func l3Fire(t *time.Timer) <-chan time.Time {
	if t == nil {
		return nil
	}
	return t.C
}

func (s *Scheduler) delayUntilNext(now, last time.Time, interval time.Duration, settle bool) time.Duration {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if !last.IsZero() {
		if rem := last.Add(interval).Sub(now); rem > 0 {
			return rem
		}
	}
	if !settle {
		return 0
	}
	first := 2 * time.Minute
	if interval < first {
		first = interval
	}
	return jitter(first, 0.3)
}

func (s *Scheduler) l2Interval() time.Duration {
	if s.L2Every > 0 {
		return s.L2Every
	}
	return 24 * time.Hour
}

func (s *Scheduler) l3Interval() time.Duration {
	if s.L3Every > 0 {
		return s.L3Every
	}
	return 60 * time.Minute
}

func (s *Scheduler) l2Workers() int {
	if s.L2Workers < 1 {
		return 4
	}
	return s.L2Workers
}

func (s *Scheduler) l3Workers() int {
	if s.L3Workers < 1 {
		return 2
	}
	return s.L3Workers
}

func (s *Scheduler) l3HealthySample() float64 {
	if s.L3HealthySample < 0 {
		return 0.1
	}
	if s.L3HealthySample > 1 {
		return 1
	}
	// Zero is a valid "never sample healthy" — but unset default is 0.1.
	// Callers must set L3HealthySample from config (default 0.1). Negative means use default.
	return s.L3HealthySample
}

func (s *Scheduler) lastL2Locked() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastL2At
}

func (s *Scheduler) lastL3Locked() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastL3At
}

func (s *Scheduler) setNextL2(at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextL2At = at.UTC()
}

func (s *Scheduler) setNextL3(at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextL3At = at.UTC()
}

func (s *Scheduler) finishL2(last, next time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastL2At = last.UTC()
	s.nextL2At = next.UTC()
	s.l2Busy = false
}

func (s *Scheduler) finishL3(last, next time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastL3At = last.UTC()
	s.nextL3At = next.UTC()
	s.l3Busy = false
}

func (s *Scheduler) runL2(ctx context.Context) {
	if s.Segment == nil {
		return
	}
	var jobs []model.Channel
	for _, feed := range s.Reg.Feeds() {
		for _, ch := range feed.Channels() {
			if ch.Excluded || ch.Classification != model.ClassNative || ProbeURL(ch) == "" {
				continue
			}
			jobs = append(jobs, ch)
		}
	}
	workers := s.l2Workers()
	slog.Info("health L2 sweep start", "candidates", len(jobs), "workers", workers)
	n := s.runJobs(ctx, jobs, workers, func(ctx context.Context, ch model.Channel) {
		check := s.probeL2(ctx, ch)
		if _, err := s.Emitter.EmitCheck(ctx, ch.Provider, ch.NormalizedID, check); err != nil {
			slog.Warn("health L2 emit", "provider", ch.Provider, "channel", ch.NormalizedID, "err", err)
		}
	})
	slog.Info("health L2 sweep done", "probed", n)
}

func (s *Scheduler) runL3(ctx context.Context) {
	if s.FFProbe == nil {
		return
	}
	sample := s.l3HealthySample()
	var jobs []model.Channel
	skippedHealthy := 0
	for _, feed := range s.Reg.Feeds() {
		for _, ch := range feed.Channels() {
			if ch.Excluded || ch.Classification == model.ClassDRM {
				continue
			}
			if ProbeURL(ch) == "" {
				continue
			}
			if ch.Classification == model.ClassBeacon && ch.EmittedURL == "" {
				continue
			}
			st := s.priorHealth(ch)
			if !l3ShouldProbe(st, sample, rand.Float64()) {
				if st == model.HealthHealthy {
					skippedHealthy++
				}
				continue
			}
			jobs = append(jobs, ch)
		}
	}
	rand.Shuffle(len(jobs), func(i, j int) { jobs[i], jobs[j] = jobs[j], jobs[i] })
	workers := s.l3Workers()
	slog.Info("health L3 sweep start",
		"candidates", len(jobs), "skipped_healthy", skippedHealthy, "workers", workers, "healthy_sample", sample)

	n := s.runJobs(ctx, jobs, workers, func(ctx context.Context, ch model.Channel) {
		check := s.probeL3(ctx, ch)
		if _, err := s.Emitter.EmitCheck(ctx, ch.Provider, ch.NormalizedID, check); err != nil {
			slog.Warn("health L3 emit", "provider", ch.Provider, "channel", ch.NormalizedID, "err", err)
		}
	})
	slog.Info("health L3 sweep done", "probed", n)
}

// l3ShouldProbe decides whether a channel is included in a scheduled L3 sweep.
// roll is in [0,1); tests pass a fixed value.
func l3ShouldProbe(status model.Health, healthySample, roll float64) bool {
	switch status {
	case model.HealthDegraded, model.HealthDown, model.HealthUntested, "":
		return true
	case model.HealthHealthy:
		if healthySample >= 1 {
			return true
		}
		if healthySample <= 0 {
			return false
		}
		return roll < healthySample
	default:
		return true
	}
}

func (s *Scheduler) priorHealth(ch model.Channel) model.Health {
	if s.Emitter != nil && s.Emitter.Store != nil {
		if raw, ok := s.Emitter.Store.Current(ch.Provider, ch.NormalizedID, channelattr.KindHealth); ok {
			var h model.ChannelHealth
			if json.Unmarshal(raw, &h) == nil {
				return h.StatusOrUntested()
			}
		}
	}
	return ch.Health.StatusOrUntested()
}

func (s *Scheduler) runJobs(ctx context.Context, jobs []model.Channel, workers int, fn func(context.Context, model.Channel)) int {
	if workers < 1 {
		workers = 1
	}
	sem := make(chan struct{}, workers)
	done := make(chan struct{})
	var pending int
	for _, ch := range jobs {
		if ctx.Err() != nil {
			break
		}
		pending++
		sem <- struct{}{}
		go func(ch model.Channel) {
			defer func() { <-sem; done <- struct{}{} }()
			fn(ctx, ch)
		}(ch)
	}
	for i := 0; i < pending; i++ {
		select {
		case <-ctx.Done():
			return pending
		case <-done:
		}
	}
	return pending
}

func (s *Scheduler) probeL2(ctx context.Context, ch model.Channel) model.HealthCheck {
	release, err := s.acquireHost(ctx, ch)
	if err != nil {
		return failCheck(model.HealthCheck{At: time.Now().UTC(), Source: "probe_l2"}, "timeout", err.Error())
	}
	defer release()
	return s.Segment.Check(ctx, ch)
}

func (s *Scheduler) probeL3(ctx context.Context, ch model.Channel) model.HealthCheck {
	release, err := s.acquireHost(ctx, ch)
	if err != nil {
		return failCheck(model.HealthCheck{At: time.Now().UTC(), Source: "probe_l3", FinalURL: ProbeURL(ch)}, "timeout", err.Error())
	}
	defer release()
	return s.FFProbe.Check(ctx, ch)
}

func (s *Scheduler) acquireHost(ctx context.Context, ch model.Channel) (func(), error) {
	if s.Hosts == nil {
		return func() {}, nil
	}
	return s.Hosts.Acquire(ctx, ProbeURL(ch))
}

// ProbeL2Now runs one L2 segment check and emits (on-demand; any class).
func (s *Scheduler) ProbeL2Now(ctx context.Context, ch model.Channel) (model.HealthCheck, model.ChannelHealth, error) {
	if s == nil || s.Segment == nil || s.Emitter == nil {
		return model.HealthCheck{}, model.ChannelHealth{}, ErrNoSegmentProber
	}
	check := s.probeL2(ctx, ch)
	health, err := s.Emitter.EmitCheck(ctx, ch.Provider, ch.NormalizedID, check)
	return check, health, err
}

// ProbeNow runs one L3 check and emits (for "Test now").
func (s *Scheduler) ProbeNow(ctx context.Context, ch model.Channel) (model.HealthCheck, model.ChannelHealth, error) {
	if s == nil || s.FFProbe == nil || s.Emitter == nil {
		return model.HealthCheck{}, model.ChannelHealth{}, ErrNoProber
	}
	check := s.probeL3(ctx, ch)
	health, err := s.Emitter.EmitCheck(ctx, ch.Provider, ch.NormalizedID, check)
	return check, health, err
}

func jitter(base time.Duration, frac float64) time.Duration {
	if base <= 0 {
		return time.Minute
	}
	if frac <= 0 {
		return base
	}
	delta := time.Duration(float64(base) * frac * (rand.Float64()*2 - 1))
	out := base + delta
	if out < time.Second {
		return time.Second
	}
	return out
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}
