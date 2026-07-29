package opsreport

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/j27-aurum/gofast/internal/channelattr"
	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

// Clock abstracts time.Now for tests.
type Clock func() time.Time

// Scheduler runs the daily official ops-report fire and exposes manual actions.
type Scheduler struct {
	Store   *config.Store
	Reg     *provider.Registry
	Attrs   *channelattr.Store
	DataDir string
	Mailer  *Mailer
	Now     Clock

	mu       sync.Mutex
	state    *stateStore
	cfg      config.OpsReport
	baseURL  string
	reconfig chan struct{}
	nextAt   time.Time
}

// Snapshot is the API/UI view of schedule + tallies.
type Snapshot struct {
	Enabled          bool                               `json:"enabled"`
	Timezone         string                             `json:"timezone"`
	SendAt           string                             `json:"send_at"`
	LastSuccessAt    time.Time                          `json:"last_success_at,omitempty"`
	LastSuccessLocal string                             `json:"last_success_local,omitempty"`
	LastError        string                             `json:"last_error,omitempty"`
	LastErrorAt      time.Time                          `json:"last_error_at,omitempty"`
	NextAt           time.Time                          `json:"next_at,omitempty"`
	RefreshTallies   map[model.ProviderID]ProviderTally `json:"refresh_tallies,omitempty"`
}

// Snapshot returns schedule state for APIs.
func (s *Scheduler) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{}
	}
	s.mu.Lock()
	cfg := s.cfg
	next := s.nextAt
	s.mu.Unlock()
	st := State{}
	if s.state != nil {
		st = s.state.snapshot()
	}
	return Snapshot{
		Enabled:          cfg.IsEnabled(),
		Timezone:         cfg.TimezoneOrDefault(),
		SendAt:           cfg.SendAtOrDefault(),
		LastSuccessAt:    st.LastSuccessAt,
		LastSuccessLocal: st.LastSuccessLocal,
		LastError:        st.LastError,
		LastErrorAt:      st.LastErrorAt,
		NextAt:           coalesceTime(next, st.NextAt),
		RefreshTallies:   st.RefreshTallies,
	}
}

func coalesceTime(a, b time.Time) time.Time {
	if !a.IsZero() {
		return a
	}
	return b
}

// Inc records a refresh outcome into durable tallies (since last official send).
func (s *Scheduler) Inc(provider model.ProviderID, ok bool) {
	if s == nil || s.state == nil {
		return
	}
	s.state.Inc(provider, ok)
}

// Reload applies a new config snapshot and wakes the Run loop.
func (s *Scheduler) Reload(ctx context.Context, cfg *config.Config) error {
	if s == nil || cfg == nil {
		return nil
	}
	s.mu.Lock()
	s.cfg = cfg.OpsReport
	s.baseURL = cfg.BaseURL
	if s.DataDir == "" {
		s.DataDir = cfg.DataDir
	}
	s.mu.Unlock()
	select {
	case s.reconfigCh() <- struct{}{}:
	default:
	}
	return nil
}

func (s *Scheduler) reconfigCh() chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reconfig == nil {
		s.reconfig = make(chan struct{}, 1)
	}
	return s.reconfig
}

func (s *Scheduler) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Scheduler) currentCfg() (config.OpsReport, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg, s.baseURL
}

func (s *Scheduler) ensureState() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != nil {
		return
	}
	dir := s.DataDir
	if dir == "" && s.Store != nil {
		if cfg := s.Store.Current(); cfg != nil {
			dir = cfg.DataDir
		}
	}
	s.state = newStateStore(dir)
}

// Run blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	if s == nil {
		return
	}
	s.ensureState()
	if err := s.state.load(); err != nil {
		slog.Warn("opsreport: load state", "err", err)
	}
	if s.Store != nil {
		if cfg := s.Store.Current(); cfg != nil {
			_ = s.Reload(ctx, cfg)
		}
	}
	if s.Mailer == nil {
		s.Mailer = &Mailer{}
	}

	timer := time.NewTimer(time.Hour)
	stopTimer(timer)
	defer timer.Stop()

	arm := func() {
		cfg, _ := s.currentCfg()
		delay, next, err := DelayUntilNext(s.now(), cfg, s.state.lastSuccess())
		if err != nil {
			slog.Warn("opsreport: schedule", "err", err)
			delay = time.Hour
		}
		s.mu.Lock()
		s.nextAt = next
		s.mu.Unlock()
		_ = s.state.setNext(next)
		stopTimer(timer)
		if !cfg.IsEnabled() {
			timer.Reset(time.Hour)
			slog.Info("opsreport schedule idle", "enabled", false)
			return
		}
		if delay < time.Second {
			delay = time.Second
		}
		timer.Reset(delay)
		slog.Info("opsreport schedule",
			"next_at", nullTime(next),
			"delay", delay.Round(time.Second).String(),
			"timezone", cfg.TimezoneOrDefault(),
			"send_at", cfg.SendAtOrDefault(),
		)
	}
	arm()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.maybeOfficial(ctx)
			arm()
		case <-s.reconfigCh():
			arm()
		}
	}
}

func (s *Scheduler) maybeOfficial(ctx context.Context) {
	cfg, _ := s.currentCfg()
	due, next, err := ShouldFire(s.now(), cfg, s.state.lastSuccess())
	if err != nil {
		slog.Warn("opsreport: should fire", "err", err)
		return
	}
	if !due {
		_ = s.state.setNext(next)
		return
	}
	if _, err := s.sendFull(ctx, KindOfficial); err != nil {
		slog.Error("opsreport: official send failed", "err", err)
		_ = s.state.recordOfficialError(s.now(), err.Error(), next)
		return
	}
}

// TestSMTP sends a short on-brand smoke message (not archived).
func (s *Scheduler) TestSMTP(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("opsreport: nil scheduler")
	}
	s.ensureState()
	cfg, base := s.currentCfg()
	if s.Store != nil {
		if cur := s.Store.Current(); cur != nil {
			cfg = cur.OpsReport
			base = cur.BaseURL
		}
	}
	if strings.TrimSpace(cfg.SMTP.Host) == "" {
		return fmt.Errorf("opsreport: smtp host is not configured")
	}
	if strings.TrimSpace(cfg.From) == "" || len(cfg.To) == 0 {
		return fmt.Errorf("opsreport: from/to are required")
	}
	rendered := RenderTestStub(base)
	mailer := s.Mailer
	if mailer == nil {
		mailer = &Mailer{}
	}
	return mailer.Send(cfg, rendered.Subject, rendered.Text, rendered.HTML)
}

// SendPreview builds and sends a full report without updating official state.
func (s *Scheduler) SendPreview(ctx context.Context) (ArchiveMeta, error) {
	if s == nil {
		return ArchiveMeta{}, fmt.Errorf("opsreport: nil scheduler")
	}
	a, err := s.sendFull(ctx, KindPreview)
	if err != nil {
		return ArchiveMeta{}, err
	}
	return ArchiveMeta{
		ID:          a.ID,
		Kind:        a.Kind,
		GeneratedAt: a.GeneratedAt,
		Subject:     a.Subject,
		Filename:    archiveFilename(a.ID),
	}, nil
}

// Resend replays a stored archive MIME (no official state change).
func (s *Scheduler) Resend(ctx context.Context, id string) error {
	if s == nil {
		return fmt.Errorf("opsreport: nil scheduler")
	}
	s.ensureState()
	cfg, _ := s.currentCfg()
	if s.Store != nil {
		if cur := s.Store.Current(); cur != nil {
			cfg = cur.OpsReport
		}
	}
	a, err := loadArchive(Dir(s.dataDir()), id)
	if err != nil {
		return err
	}
	mailer := s.Mailer
	if mailer == nil {
		mailer = &Mailer{}
	}
	return mailer.Send(cfg, a.Subject, a.Text, a.HTML)
}

// ListArchives returns recent archived reports.
func (s *Scheduler) ListArchives(limit int) ([]ArchiveMeta, error) {
	if s == nil {
		return nil, nil
	}
	s.ensureState()
	return listArchives(Dir(s.dataDir()), limit)
}

// GetArchive loads one archived report.
func (s *Scheduler) GetArchive(id string) (Archive, error) {
	if s == nil {
		return Archive{}, fmt.Errorf("opsreport: nil scheduler")
	}
	s.ensureState()
	return loadArchive(Dir(s.dataDir()), id)
}

func (s *Scheduler) sendFull(ctx context.Context, kind Kind) (Archive, error) {
	s.ensureState()
	cfg, base := s.currentCfg()
	if s.Store != nil {
		if cur := s.Store.Current(); cur != nil {
			cfg = cur.OpsReport
			base = cur.BaseURL
			s.mu.Lock()
			s.cfg = cfg
			s.baseURL = base
			s.mu.Unlock()
		}
	}
	if strings.TrimSpace(cfg.SMTP.Host) == "" {
		return Archive{}, fmt.Errorf("opsreport: smtp host is not configured")
	}
	if strings.TrimSpace(cfg.From) == "" || len(cfg.To) == 0 {
		return Archive{}, fmt.Errorf("opsreport: from/to are required")
	}

	composer := &Composer{
		Reg:   s.Reg,
		Attrs: s.Attrs,
		Cfg: func() *config.Config {
			if s.Store != nil {
				return s.Store.Current()
			}
			return &config.Config{OpsReport: cfg, BaseURL: base}
		},
	}
	now := s.now()
	rep, err := composer.Build(ctx, kind, now, s.state.lastSuccess(), s.state.tallies())
	if err != nil {
		return Archive{}, err
	}
	rendered := Render(rep)
	mailer := s.Mailer
	if mailer == nil {
		mailer = &Mailer{}
	}
	if err := mailer.Send(cfg, rendered.Subject, rendered.Text, rendered.HTML); err != nil {
		return Archive{}, err
	}

	a, err := writeArchive(Dir(s.dataDir()), kind, now, rendered.Subject, rendered.Text, rendered.HTML, rep)
	if err != nil {
		slog.Warn("opsreport: archive write failed after successful send", "err", err)
	}

	if kind == KindOfficial {
		loc, _ := cfg.Location()
		local := FormatLocalDate(now, loc)
		_, next, _ := DelayUntilNext(now.Add(time.Minute), cfg, now)
		if next.IsZero() {
			hour, minute, _ := config.ParseSendAt(cfg.SendAtOrDefault())
			if loc == nil {
				loc = time.UTC
			}
			next = NextFire(now.Add(time.Minute), loc, hour, minute)
		}
		if err := s.state.recordOfficialSuccess(now, local, next); err != nil {
			slog.Warn("opsreport: persist last success", "err", err)
		}
		s.mu.Lock()
		s.nextAt = next
		s.mu.Unlock()
		slog.Info("opsreport: official send ok", "local_date", local, "next_at", next.UTC())
	} else {
		slog.Info("opsreport: preview send ok", "archive_id", a.ID)
	}
	return a, nil
}

func (s *Scheduler) dataDir() string {
	s.mu.Lock()
	dir := s.DataDir
	s.mu.Unlock()
	if dir != "" {
		return dir
	}
	if s.Store != nil {
		if cfg := s.Store.Current(); cfg != nil {
			return cfg.DataDir
		}
	}
	return config.DefaultDataDir
}

func stopTimer(t *time.Timer) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}
