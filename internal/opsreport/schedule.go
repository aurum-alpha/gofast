package opsreport

import (
	"fmt"
	"time"

	"github.com/j27-aurum/gofast/internal/config"
)

const graceWindow = 2 * time.Hour

// NextFire returns the next scheduled send instant in UTC for the given local
// wall clock in loc. If today's slot is still in the future, use today; else
// tomorrow. now is interpreted in loc for calendar-day math.
func NextFire(now time.Time, loc *time.Location, hour, minute int) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	local := now.In(loc)
	candidate := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, loc)
	if !candidate.After(local) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate.UTC()
}

// ShouldFire reports whether an official send is due: enabled schedule, not
// already successfully sent for this local calendar day, and either at/after
// today's send_at or still within the grace window after it.
func ShouldFire(now time.Time, cfg config.OpsReport, lastSuccess time.Time) (bool, time.Time, error) {
	if !cfg.IsEnabled() {
		return false, time.Time{}, nil
	}
	loc, err := cfg.Location()
	if err != nil {
		return false, time.Time{}, err
	}
	hour, minute, err := config.ParseSendAt(cfg.SendAtOrDefault())
	if err != nil {
		return false, time.Time{}, err
	}
	local := now.In(loc)
	todaySlot := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, loc)
	next := NextFire(now, loc, hour, minute)

	if !lastSuccess.IsZero() {
		ls := lastSuccess.In(loc)
		if sameLocalDay(ls, local) {
			return false, next, nil
		}
	}
	if local.Before(todaySlot) {
		return false, next, nil
	}
	if local.Sub(todaySlot) > graceWindow {
		// Missed the grace window — wait for tomorrow's slot.
		return false, next, nil
	}
	return true, next, nil
}

func sameLocalDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// DelayUntilNext returns how long to wait until the next fire (or grace catch-up).
func DelayUntilNext(now time.Time, cfg config.OpsReport, lastSuccess time.Time) (time.Duration, time.Time, error) {
	due, next, err := ShouldFire(now, cfg, lastSuccess)
	if err != nil {
		return 0, time.Time{}, err
	}
	if due {
		return 0, next, nil
	}
	if !cfg.IsEnabled() {
		return time.Hour, time.Time{}, nil
	}
	loc, err := cfg.Location()
	if err != nil {
		return 0, time.Time{}, err
	}
	hour, minute, err := config.ParseSendAt(cfg.SendAtOrDefault())
	if err != nil {
		return 0, time.Time{}, err
	}
	next = NextFire(now, loc, hour, minute)
	d := next.Sub(now.UTC())
	if d < 0 {
		d = 0
	}
	return d, next, nil
}

// FormatLocalDate formats t in loc as YYYY-MM-DD for subjects.
func FormatLocalDate(t time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	return t.In(loc).Format("2006-01-02")
}

// Subject builds the email subject line.
func Subject(kind Kind, localDate, zone string) string {
	base := fmt.Sprintf("GoFAST ops report — %s", localDate)
	switch kind {
	case KindPreview:
		return "[Preview] " + base
	case KindTest:
		return "GoFAST SMTP test"
	default:
		return base
	}
}
