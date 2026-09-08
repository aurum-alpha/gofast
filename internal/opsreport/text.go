package opsreport

import (
	"fmt"
	"strings"
)

// RenderText builds the plain-text multipart alternative.
func RenderText(rep Report) string {
	var b strings.Builder
	title := "GoFAST daily ops report"
	if rep.Kind == KindPreview {
		title = "GoFAST preview ops report"
	}
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString(strings.Repeat("=", len(title)))
	b.WriteString("\n")
	fmt.Fprintf(&b, "Local date: %s (%s)\n", rep.LocalDate, rep.Timezone)
	fmt.Fprintf(&b, "Window: %s → %s UTC\n\n", fmtTime(rep.WindowStart), fmtTime(rep.WindowEnd))

	b.WriteString("System health\n-------------\n")
	fmt.Fprintf(&b, "healthy=%d degraded=%d down=%d untested=%d\n",
		rep.Health.Healthy, rep.Health.Degraded, rep.Health.Down, rep.Health.Untested)
	for _, w := range rep.Health.Worst {
		fmt.Fprintf(&b, "  %s %s · %s\n", w.Status, w.Provider, displayName(w.Name, w.ChannelID))
	}
	b.WriteString("\n")

	b.WriteString("Providers\n---------\n")
	if len(rep.Providers) == 0 {
		b.WriteString("(none)\n")
	}
	for _, p := range rep.Providers {
		en := "disabled"
		if p.Enabled {
			en = providerStatusLine(p)
		}
		fmt.Fprintf(&b, "%s [%s] enabled=%v status=%s exported=%d refresh_ok=%d refresh_fail=%d guide_h=%.1f fetched=%s\n",
			p.Label, p.ID, p.Enabled, en, p.Exported, p.RefreshOK, p.RefreshFail, p.GuideHoursAhead, fmtTime(p.FetchedAt))
		if p.LastError != "" {
			fmt.Fprintf(&b, "  last_error: %s\n", p.LastError)
		}
	}
	b.WriteString("\n")

	b.WriteString("Channel deltas\n--------------\n")
	fmt.Fprintf(&b, "Added (%d)\n", len(rep.Added))
	if len(rep.Added) == 0 {
		b.WriteString("  none in window\n")
	}
	for _, r := range rep.Added {
		fmt.Fprintf(&b, "  + %s · %s @ %s\n", r.Provider, displayName(r.Name, r.ChannelID), fmtTime(r.At))
	}
	fmt.Fprintf(&b, "Dropped (%d)\n", len(rep.Dropped))
	if len(rep.Dropped) == 0 {
		b.WriteString("  none in window\n")
	}
	for _, r := range rep.Dropped {
		fmt.Fprintf(&b, "  - %s · %s @ %s\n", r.Provider, displayName(r.Name, r.ChannelID), fmtTime(r.At))
	}
	fmt.Fprintf(&b, "Classification changes (%d)\n", len(rep.ClassChanges))
	if len(rep.ClassChanges) == 0 {
		b.WriteString("  none in window\n")
	}
	for _, r := range rep.ClassChanges {
		fmt.Fprintf(&b, "  ~ %s · %s: %s → %s\n", r.Provider, displayName(r.Name, r.ChannelID), emptyDash(r.Old), emptyDash(r.New))
	}
	fmt.Fprintf(&b, "Health transitions (%d)\n", len(rep.HealthChanges))
	if len(rep.HealthChanges) == 0 {
		b.WriteString("  none in window\n")
	}
	for _, r := range rep.HealthChanges {
		fmt.Fprintf(&b, "  ! %s · %s: %s → %s\n", r.Provider, displayName(r.Name, r.ChannelID), r.Old, r.New)
	}

	if rep.BaseURL != "" {
		b.WriteString("\nStatus: ")
		b.WriteString(strings.TrimRight(rep.BaseURL, "/"))
		b.WriteString("/\n")
	}
	return b.String()
}
