package health

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDelayUntilNextFromLast(t *testing.T) {
	s := &Scheduler{L2Every: 24 * time.Hour}
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	last := now.Add(-6 * time.Hour)
	got := s.delayUntilNext(now, last, s.l2Interval(), true)
	want := 18 * time.Hour
	if got != want {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestDelayUntilNextOverdueSettles(t *testing.T) {
	s := &Scheduler{L2Every: 24 * time.Hour}
	now := time.Now()
	last := now.Add(-25 * time.Hour)
	got := s.delayUntilNext(now, last, s.l2Interval(), true)
	if got < time.Minute || got > 3*time.Minute {
		t.Fatalf("overdue settle = %v", got)
	}
}

func TestDelayUntilNextColdStartSettles(t *testing.T) {
	s := &Scheduler{L2Every: 24 * time.Hour}
	got := s.delayUntilNext(time.Now(), time.Time{}, s.l2Interval(), true)
	if got < time.Minute || got > 3*time.Minute {
		t.Fatalf("cold settle = %v", got)
	}
}

func TestScheduleStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "health_schedule.json")
	s := &Scheduler{StatePath: path}
	last := time.Date(2026, 7, 23, 20, 0, 0, 0, time.UTC)
	next := last.Add(24 * time.Hour)
	s.finishL2(last, next)
	s.saveState()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f scheduleFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if !f.LastL2At.Equal(last) || !f.NextL2At.Equal(next) {
		t.Fatalf("file = %+v", f)
	}

	s2 := &Scheduler{StatePath: path, L2Every: 24 * time.Hour}
	s2.loadState()
	snap := s2.Snapshot()
	if !snap.LastL2At.Equal(last) || !snap.NextL2At.Equal(next) {
		t.Fatalf("snapshot = %+v", snap)
	}
	if snap.L2Interval != "24h0m0s" {
		t.Fatalf("interval = %q", snap.L2Interval)
	}
}
