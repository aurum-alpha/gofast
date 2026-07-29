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
	b.WriteString(fmt.Sprintf("Local date: %s (%s)\n", rep.LocalDate, rep.Timezone))
	b.WriteString(fmt.Sprintf("Window: %s → %s UTC\n\n", fmtTime(rep.WindowStart), fmtTime(rep.WindowEnd)))

	b.WriteString("Fleet health\n------------\n")
	b.WriteString(fmt.Sprintf("healthy=%d degraded=%d down=%d untested=%d\n",
		rep.Health.Healthy, rep.Health.Degraded, rep.Health.Down, rep.Health.Untested))
	for _, w := range rep.Health.Worst {
		b.WriteString(fmt.Sprintf("  %s %s (%s) %s\n", w.Status, w.Provider, w.ChannelID, displayName(w.Name, w.ChannelID)))
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
		b.WriteString(fmt.Sprintf("%s [%s] enabled=%v status=%s exported=%d refresh_ok=%d refresh_fail=%d guide_h=%.1f fetched=%s\n",
			p.Label, p.ID, p.Enabled, en, p.Exported, p.RefreshOK, p.RefreshFail, p.GuideHoursAhead, fmtTime(p.FetchedAt)))
		if p.LastError != "" {
			b.WriteString(fmt.Sprintf("  last_error: %s\n", p.LastError))
		}
	}
	b.WriteString("\n")

	b.WriteString("Channel deltas\n--------------\n")
	b.WriteString(fmt.Sprintf("Added (%d)\n", len(rep.Added)))
	if len(rep.Added) == 0 {
		b.WriteString("  none in window\n")
	}
	for _, r := range rep.Added {
		b.WriteString(fmt.Sprintf("  + %s %s (%s)\n", r.Provider, displayName(r.Name, r.ChannelID), r.ChannelID))
	}
	b.WriteString(fmt.Sprintf("Dropped (%d)\n", len(rep.Dropped)))
	if len(rep.Dropped) == 0 {
		b.WriteString("  none in window\n")
	}
	for _, r := range rep.Dropped {
		b.WriteString(fmt.Sprintf("  - %s %s (%s)\n", r.Provider, displayName(r.Name, r.ChannelID), r.ChannelID))
	}
	b.WriteString(fmt.Sprintf("Classification changes (%d)\n", len(rep.ClassChanges)))
	if len(rep.ClassChanges) == 0 {
		b.WriteString("  none in window\n")
	}
	for _, r := range rep.ClassChanges {
		b.WriteString(fmt.Sprintf("  ~ %s %s: %s → %s\n", r.Provider, r.ChannelID, emptyDash(r.Old), emptyDash(r.New)))
	}
	b.WriteString(fmt.Sprintf("Health transitions (%d)\n", len(rep.HealthChanges)))
	if len(rep.HealthChanges) == 0 {
		b.WriteString("  none in window\n")
	}
	for _, r := range rep.HealthChanges {
		b.WriteString(fmt.Sprintf("  ! %s %s: %s → %s\n", r.Provider, r.ChannelID, r.Old, r.New))
	}

	if rep.BaseURL != "" {
		b.WriteString("\nStatus: ")
		b.WriteString(strings.TrimRight(rep.BaseURL, "/"))
		b.WriteString("/\n")
	}
	return b.String()
}
