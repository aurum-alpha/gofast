package health

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"time"

	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

var errNoProber = errors.New("health: L3 prober not configured")

// Scheduler runs L2 (daily NATIVE segment) and optional L3 (ffprobe) loops.
type Scheduler struct {
	Reg       *provider.Registry
	Emitter   *Emitter
	Segment   *SegmentProber
	FFProbe   *FFProbe
	L2Every   time.Duration
	L3Every   time.Duration // jitter window when L3Enabled
	L3On      bool
	L3Workers int
}

// Run blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	if s == nil || s.Reg == nil || s.Emitter == nil {
		return
	}
	l2 := s.L2Every
	if l2 <= 0 {
		l2 = 24 * time.Hour
	}
	// First L2 after a short delay so boot/refresh settle.
	firstL2 := 2 * time.Minute
	if l2 < firstL2 {
		firstL2 = l2
	}
	l2Timer := time.NewTimer(jitter(firstL2, 0.3))
	defer l2Timer.Stop()

	var l3Timer *time.Timer
	if s.L3On {
		l3Every := s.L3Every
		if l3Every <= 0 {
			l3Every = 60 * time.Minute
		}
		l3Timer = time.NewTimer(jitter(l3Every, 1.0))
		defer l3Timer.Stop()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-l2Timer.C:
			s.runL2(ctx)
			l2Timer.Reset(jitter(l2, 0.1))
		case <-l3Fire(l3Timer):
			if s.L3On && s.FFProbe != nil {
				s.runL3(ctx)
				l3Every := s.L3Every
				if l3Every <= 0 {
					l3Every = 60 * time.Minute
				}
				l3Timer.Reset(jitter(l3Every, 1.0))
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

func (s *Scheduler) runL2(ctx context.Context) {
	if s.Segment == nil {
		return
	}
	slog.Info("health L2 sweep start")
	n := 0
	for _, feed := range s.Reg.Feeds() {
		for _, ch := range feed.Channels() {
			if ctx.Err() != nil {
				return
			}
			if ch.Excluded || ch.Classification != model.ClassNative || ch.StreamURL == "" {
				continue
			}
			check := s.Segment.Check(ctx, ch)
			if err := s.Emitter.EmitCheck(ctx, ch.Provider, ch.NormalizedID, check); err != nil {
				slog.Warn("health L2 emit", "provider", ch.Provider, "channel", ch.NormalizedID, "err", err)
			}
			n++
		}
	}
	slog.Info("health L2 sweep done", "probed", n)
}

func (s *Scheduler) runL3(ctx context.Context) {
	if s.FFProbe == nil {
		return
	}
	workers := s.L3Workers
	if workers < 1 {
		workers = 2
	}
	type job struct {
		ch model.Channel
	}
	var jobs []job
	for _, feed := range s.Reg.Feeds() {
		for _, ch := range feed.Channels() {
			if ch.Excluded || ch.Classification == model.ClassDRM {
				continue
			}
			if ProbeURL(ch) == "" {
				continue
			}
			// BEACON only when EmittedURL is proxy (end-to-end); skip if no proxy URL.
			if ch.Classification == model.ClassBeacon && ch.EmittedURL == "" {
				continue
			}
			jobs = append(jobs, job{ch: ch})
		}
	}
	rand.Shuffle(len(jobs), func(i, j int) { jobs[i], jobs[j] = jobs[j], jobs[i] })
	slog.Info("health L3 sweep start", "candidates", len(jobs), "workers", workers)

	sem := make(chan struct{}, workers)
	done := make(chan struct{})
	var pending int
	for _, j := range jobs {
		if ctx.Err() != nil {
			break
		}
		pending++
		sem <- struct{}{}
		go func(ch model.Channel) {
			defer func() { <-sem; done <- struct{}{} }()
			check := s.FFProbe.Check(ctx, WithProbeURL(ch))
			if err := s.Emitter.EmitCheck(ctx, ch.Provider, ch.NormalizedID, check); err != nil {
				slog.Warn("health L3 emit", "provider", ch.Provider, "channel", ch.NormalizedID, "err", err)
			}
		}(j.ch)
	}
	for i := 0; i < pending; i++ {
		select {
		case <-ctx.Done():
			return
		case <-done:
		}
	}
	slog.Info("health L3 sweep done", "probed", pending)
}

// ProbeNow runs one L3 check and emits (for J27-26 "Test now").
func (s *Scheduler) ProbeNow(ctx context.Context, ch model.Channel) (model.HealthCheck, error) {
	if s == nil || s.FFProbe == nil || s.Emitter == nil {
		return model.HealthCheck{}, errNoProber
	}
	check := s.FFProbe.Check(ctx, WithProbeURL(ch))
	err := s.Emitter.EmitCheck(ctx, ch.Provider, ch.NormalizedID, check)
	return check, err
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
