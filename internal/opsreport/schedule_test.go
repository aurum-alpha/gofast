package opsreport

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/model"
)

func TestNextFireAndAlreadySent(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	// 2026-07-28 00:30 PDT
	now := time.Date(2026, 7, 28, 0, 30, 0, 0, loc)
	next := NextFire(now, loc, 0, 0)
	want := time.Date(2026, 7, 29, 0, 0, 0, 0, loc).UTC()
	if !next.Equal(want) {
		t.Fatalf("next=%v want %v", next, want)
	}

	cfg := config.OpsReport{
		Enabled:  boolPtr(true),
		Timezone: "America/Los_Angeles",
		SendAt:   "00:00",
	}
	// Within grace after midnight, not yet sent → due.
	due, _, err := ShouldFire(now, cfg, time.Time{})
	if err != nil || !due {
		t.Fatalf("expected due, due=%v err=%v", due, err)
	}
	// Already sent today → not due.
	sent := time.Date(2026, 7, 28, 0, 5, 0, 0, loc)
	due, next, err = ShouldFire(now, cfg, sent)
	if err != nil || due {
		t.Fatalf("expected not due after send, due=%v err=%v", due, err)
	}
	if !next.Equal(want) {
		t.Fatalf("next after send=%v want %v", next, want)
	}

	// Past grace (3h after midnight) → skip to tomorrow.
	late := time.Date(2026, 7, 28, 3, 30, 0, 0, loc)
	due, _, err = ShouldFire(late, cfg, time.Time{})
	if err != nil || due {
		t.Fatalf("past grace should not fire, due=%v err=%v", due, err)
	}
}

func TestTallyIncResetOnOfficial(t *testing.T) {
	dir := t.TempDir()
	st := newStateStore(dir)
	st.Inc("lg", true)
	st.Inc("lg", false)
	st.Inc("pluto", true)
	snap := st.snapshot()
	if snap.RefreshTallies["lg"].Successes != 1 || snap.RefreshTallies["lg"].Failures != 1 {
		t.Fatalf("tallies: %+v", snap.RefreshTallies)
	}
	next := time.Now().UTC().Add(24 * time.Hour)
	if err := st.recordOfficialSuccess(time.Now(), "2026-07-28", next); err != nil {
		t.Fatal(err)
	}
	snap = st.snapshot()
	if len(snap.RefreshTallies) != 0 {
		t.Fatalf("tallies should reset: %+v", snap.RefreshTallies)
	}
	if snap.LastSuccessLocal != "2026-07-28" {
		t.Fatalf("local=%q", snap.LastSuccessLocal)
	}
	// Reload from disk.
	st2 := newStateStore(dir)
	if err := st2.load(); err != nil {
		t.Fatal(err)
	}
	if st2.snapshot().LastSuccessLocal != "2026-07-28" {
		t.Fatal("persist failed")
	}
}

func TestArchiveWritePruneResendPayload(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ops_reports")
	rep := Report{Kind: KindPreview, LocalDate: "2026-07-28", Timezone: "UTC"}
	a, err := writeArchive(dir, KindPreview, time.Date(2026, 7, 28, 7, 0, 0, 0, time.UTC),
		"subj", "text body", "<html>GoFAST</html>", rep)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadArchive(dir, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Subject != "subj" || loaded.HTML != "<html>GoFAST</html>" || loaded.Text != "text body" {
		t.Fatalf("loaded=%+v", loaded)
	}
	list, err := listArchives(dir, 10)
	if err != nil || len(list) != 1 || list[0].ID != a.ID {
		t.Fatalf("list=%v err=%v", list, err)
	}
	// Old file pruned.
	oldPath := filepath.Join(dir, "report-20200101T000000Z.json")
	if err := os.WriteFile(oldPath, []byte(`{"id":"20200101T000000Z"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-100 * 24 * time.Hour)
	_ = os.Chtimes(oldPath, oldTime, oldTime)
	_ = pruneArchives(dir, time.Now().Add(-archiveRetention))
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expected prune, err=%v", err)
	}
}

func TestRenderHTMLContainsBrand(t *testing.T) {
	html := RenderHTML(Report{
		Kind:      KindOfficial,
		LocalDate: "2026-07-28",
		Timezone:  "America/Los_Angeles",
		BaseURL:   "http://localhost:8180",
		Health:    HealthRollup{Healthy: 10, Degraded: 1, Down: 0, Untested: 2},
		Providers: []ProviderRow{{ID: "lg", Label: "LG Channels", Enabled: true, Exported: 42}},
		Added:     []DeltaRow{{Provider: "lg", ChannelID: "abc", Name: "Demo"}},
	})
	for _, want := range []string{"GoFAST", "#00a4dc", "#101014", "#e8ebef", "#1a1c22", "System health", "LG Channels", "Demo", "Open Status", `color-scheme" content="dark`} {
		if !contains(html, want) {
			t.Fatalf("html missing %q", want)
		}
	}
	text := RenderText(Report{Kind: KindOfficial, LocalDate: "2026-07-28", Timezone: "UTC", Added: []DeltaRow{}})
	if !contains(text, "none in window") {
		t.Fatal("text should note empty deltas")
	}
}

func TestPreviewDoesNotResetTallies(t *testing.T) {
	dir := t.TempDir()
	sched := &Scheduler{
		DataDir: dir,
		Mailer:  &Mailer{DialTimeout: time.Millisecond}, // will fail dial — exercise pre-send state
		Now:     func() time.Time { return time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC) },
		cfg: config.OpsReport{
			Enabled: boolPtr(true),
			From:    "a@example.com",
			To:      []string{"b@example.com"},
			SMTP:    config.OpsReportSMTP{Host: "127.0.0.1", Port: 1},
		},
	}
	sched.ensureState()
	sched.state.Inc("lg", true)
	_, err := sched.SendPreview(context.Background())
	if err == nil {
		t.Fatal("expected smtp failure")
	}
	if sched.state.tallies()["lg"].Successes != 1 {
		t.Fatalf("preview must not reset tallies on failure: %+v", sched.state.tallies())
	}
	_ = model.ProviderID("lg")
}

func boolPtr(v bool) *bool { return &v }

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		})())
}
