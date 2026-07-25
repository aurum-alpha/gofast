package model

import (
	"strings"
	"time"
)

// Programme is a guide entry keyed by NormalizedID of its channel.
type Programme struct {
	ChannelID string    `json:"channel_id"` // normalized channel id
	Title     string    `json:"title"`
	Desc      string    `json:"desc,omitempty"`
	Start     time.Time `json:"start"`
	Stop      time.Time `json:"stop"`
	// Categories are upstream XMLTV <category> labels (never mutated by taxonomy).
	Categories []string `json:"categories,omitempty"`
	// EmittedCategories are post-taxonomy labels for XMLTV emit and UI.
	// Empty means "use Categories" (taxonomy off or not applied).
	EmittedCategories []string `json:"emitted_categories,omitempty"`
}

// IsValid reports whether a programme is eligible for guide emission.
func (p Programme) IsValid() bool {
	return strings.TrimSpace(p.Title) != "" &&
		!p.Start.IsZero() &&
		!p.Stop.IsZero() &&
		p.Stop.After(p.Start)
}

// ExportCategories returns labels for XMLTV/UI: EmittedCategories when set,
// otherwise upstream Categories.
func (p Programme) ExportCategories() []string {
	if len(p.EmittedCategories) > 0 {
		return p.EmittedCategories
	}
	return p.Categories
}
