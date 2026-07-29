package opsreport

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/j27-aurum/gofast/internal/channelattr"
	"github.com/j27-aurum/gofast/internal/config"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider"
)

const (
	deltaLimitPerKind = 500
	healthDeltaCap    = 100
	worstHealthCap    = 15
)

// Report is the structured digest payload (archived + rendered).
type Report struct {
	Kind          Kind             `json:"kind"`
	GeneratedAt   time.Time        `json:"generated_at"`
	WindowStart   time.Time        `json:"window_start"`
	WindowEnd     time.Time        `json:"window_end"`
	Timezone      string           `json:"timezone"`
	LocalDate     string           `json:"local_date"`
	BaseURL       string           `json:"base_url,omitempty"`
	Providers     []ProviderRow    `json:"providers"`
	Health        HealthRollup     `json:"health"`
	Added         []DeltaRow       `json:"added"`
	Dropped       []DeltaRow       `json:"dropped"`
	ClassChanges  []ClassDeltaRow  `json:"class_changes"`
	HealthChanges []HealthDeltaRow `json:"health_changes"`
}

// ProviderRow is one provider's snapshot line.
type ProviderRow struct {
	ID              model.ProviderID `json:"id"`
	Label           string           `json:"label"`
	Enabled         bool             `json:"enabled"`
	FetchedAt       time.Time        `json:"fetched_at,omitempty"`
	LastError       string           `json:"last_error,omitempty"`
	Exported        int              `json:"exported_channels"`
	GuideHoursAhead float64          `json:"guide_hours_ahead,omitempty"`
	IntervalClamped bool             `json:"interval_clamped,omitempty"`
	RefreshOK       uint64           `json:"refresh_successes"`
	RefreshFail     uint64           `json:"refresh_failures"`
}

// HealthRollup counts fleet health statuses.
type HealthRollup struct {
	Healthy  int        `json:"healthy"`
	Degraded int        `json:"degraded"`
	Down     int        `json:"down"`
	Untested int        `json:"untested"`
	Worst    []WorstRow `json:"worst,omitempty"`
}

// WorstRow is a degraded/down channel for the top-N list.
type WorstRow struct {
	Provider  model.ProviderID `json:"provider"`
	ChannelID string           `json:"channel_id"`
	Name      string           `json:"name"`
	Status    model.Health     `json:"status"`
}

// DeltaRow is an add/drop presence event.
type DeltaRow struct {
	Provider  model.ProviderID `json:"provider"`
	ChannelID string           `json:"channel_id"`
	Name      string           `json:"name"`
	At        time.Time        `json:"at"`
}

// ClassDeltaRow is a classification change.
type ClassDeltaRow struct {
	Provider  model.ProviderID `json:"provider"`
	ChannelID string           `json:"channel_id"`
	Name      string           `json:"name,omitempty"`
	Old       string           `json:"old"`
	New       string           `json:"new"`
	At        time.Time        `json:"at"`
}

// HealthDeltaRow is a health status transition.
type HealthDeltaRow struct {
	Provider  model.ProviderID `json:"provider"`
	ChannelID string           `json:"channel_id"`
	Name      string           `json:"name,omitempty"`
	Old       model.Health     `json:"old"`
	New       model.Health     `json:"new"`
	At        time.Time        `json:"at"`
}

// Composer builds Report snapshots from live feeds + channelattr history.
type Composer struct {
	Reg   *provider.Registry
	Attrs *channelattr.Store
	Cfg   func() *config.Config
}

func (c *Composer) Build(ctx context.Context, kind Kind, now time.Time, lastSuccess time.Time, tallies map[model.ProviderID]ProviderTally) (Report, error) {
	cfg := &config.Config{}
	if c.Cfg != nil {
		if cur := c.Cfg(); cur != nil {
			cfg = cur
		}
	}
	ops := cfg.OpsReport
	loc, err := ops.Location()
	if err != nil {
		loc = time.UTC
	}
	windowStart := lastSuccess
	if windowStart.IsZero() {
		windowStart = now.Add(-24 * time.Hour)
	}
	rep := Report{
		Kind:        kind,
		GeneratedAt: now.UTC(),
		WindowStart: windowStart.UTC(),
		WindowEnd:   now.UTC(),
		Timezone:    ops.TimezoneOrDefault(),
		LocalDate:   FormatLocalDate(now, loc),
		BaseURL:     strings.TrimRight(cfg.BaseURL, "/"),
	}

	rep.Providers = c.providerRows(tallies)
	rep.Health = c.healthRollup()

	if c.Attrs != nil {
		added, dropped, err := c.presenceDeltas(ctx, windowStart)
		if err != nil {
			return Report{}, err
		}
		rep.Added, rep.Dropped = added, dropped
		rep.ClassChanges, err = c.classDeltas(ctx, windowStart)
		if err != nil {
			return Report{}, err
		}
		rep.HealthChanges, err = c.healthDeltas(ctx, windowStart)
		if err != nil {
			return Report{}, err
		}
	}
	return rep, nil
}

func (c *Composer) providerRows(tallies map[model.ProviderID]ProviderTally) []ProviderRow {
	if c.Reg == nil {
		return nil
	}
	list := c.Reg.Providers()
	out := make([]ProviderRow, 0, len(list.Providers))
	for _, p := range list.Providers {
		row := ProviderRow{
			ID:      p.ID,
			Label:   p.Label,
			Enabled: p.IsEnabled(),
		}
		if row.Label == "" {
			row.Label = string(p.ID)
		}
		if t, ok := tallies[p.ID]; ok {
			row.RefreshOK = t.Successes
			row.RefreshFail = t.Failures
		}
		if feed, ok := c.Reg.Feed(p.ID); ok && feed != nil {
			stats := feed.Stats()
			row.FetchedAt = stats.FetchedAt
			row.LastError = stats.LastError
			row.Exported = stats.ExportedChannels
			row.GuideHoursAhead = stats.GuideHoursAhead
			row.IntervalClamped = stats.RefreshIntervalClamped
		}
		out = append(out, row)
	}
	return out
}

func (c *Composer) healthRollup() HealthRollup {
	var out HealthRollup
	if c.Reg == nil {
		return out
	}
	var worst []WorstRow
	for _, ch := range c.Reg.Channels() {
		if ch.Excluded && ch.Presence == channelattr.PresenceAbsent {
			continue
		}
		st := ch.Health.Status
		if st == "" {
			st = model.HealthUntested
		}
		switch st {
		case model.HealthHealthy:
			out.Healthy++
		case model.HealthDegraded:
			out.Degraded++
			worst = append(worst, WorstRow{Provider: ch.Provider, ChannelID: ch.NormalizedID, Name: ch.Name, Status: st})
		case model.HealthDown:
			out.Down++
			worst = append(worst, WorstRow{Provider: ch.Provider, ChannelID: ch.NormalizedID, Name: ch.Name, Status: st})
		default:
			out.Untested++
		}
	}
	sort.Slice(worst, func(i, j int) bool {
		if worst[i].Status != worst[j].Status {
			return worst[i].Status == model.HealthDown
		}
		if worst[i].Provider != worst[j].Provider {
			return worst[i].Provider < worst[j].Provider
		}
		return worst[i].ChannelID < worst[j].ChannelID
	})
	if len(worst) > worstHealthCap {
		worst = worst[:worstHealthCap]
	}
	out.Worst = worst
	return out
}

func (c *Composer) presenceDeltas(ctx context.Context, since time.Time) (added, dropped []DeltaRow, err error) {
	events, err := c.Attrs.EventsSince(ctx, since, []channelattr.Kind{channelattr.KindPresence}, "", deltaLimitPerKind)
	if err != nil {
		return nil, nil, err
	}
	for _, ev := range events {
		var p channelattr.Presence
		if json.Unmarshal(ev.Value, &p) != nil {
			continue
		}
		name := p.Name
		if name == "" {
			name = ev.ChannelID
		}
		row := DeltaRow{Provider: ev.Provider, ChannelID: ev.ChannelID, Name: name, At: ev.At}
		switch p.State {
		case channelattr.PresencePresent:
			added = append(added, row)
		case channelattr.PresenceAbsent:
			dropped = append(dropped, row)
		}
	}
	return added, dropped, nil
}

func (c *Composer) classDeltas(ctx context.Context, since time.Time) ([]ClassDeltaRow, error) {
	events, err := c.Attrs.EventsSince(ctx, since, []channelattr.Kind{channelattr.KindClassification}, "", deltaLimitPerKind)
	if err != nil {
		return nil, err
	}
	var out []ClassDeltaRow
	for _, ev := range events {
		var neu string
		if json.Unmarshal(ev.Value, &neu) != nil {
			neu = strings.Trim(string(ev.Value), `"`)
		}
		old := ""
		hist, herr := c.Attrs.History(ctx, ev.Provider, ev.ChannelID, channelattr.KindClassification, 2)
		if herr == nil && len(hist) >= 2 {
			_ = json.Unmarshal(hist[1].Value, &old)
			if old == "" {
				old = strings.Trim(string(hist[1].Value), `"`)
			}
		}
		if old == neu {
			continue
		}
		out = append(out, ClassDeltaRow{
			Provider:  ev.Provider,
			ChannelID: ev.ChannelID,
			Old:       old,
			New:       neu,
			At:        ev.At,
		})
	}
	return out, nil
}

func (c *Composer) healthDeltas(ctx context.Context, since time.Time) ([]HealthDeltaRow, error) {
	events, err := c.Attrs.EventsSince(ctx, since, []channelattr.Kind{channelattr.KindHealth}, "", deltaLimitPerKind)
	if err != nil {
		return nil, err
	}
	type key struct {
		p model.ProviderID
		c string
	}
	last := map[key]model.Health{}
	var transitions []HealthDeltaRow
	for _, ev := range events {
		var h model.ChannelHealth
		if json.Unmarshal(ev.Value, &h) != nil {
			continue
		}
		k := key{ev.Provider, ev.ChannelID}
		prev, ok := last[k]
		last[k] = h.Status
		if !ok {
			hist, herr := c.Attrs.History(ctx, ev.Provider, ev.ChannelID, channelattr.KindHealth, 20)
			if herr == nil {
				for _, he := range hist {
					if !he.At.Before(ev.At) {
						continue
					}
					var older model.ChannelHealth
					if json.Unmarshal(he.Value, &older) == nil {
						prev = older.Status
						ok = true
					}
					break
				}
			}
		}
		if !ok || prev == h.Status || h.Status == "" {
			continue
		}
		transitions = append(transitions, HealthDeltaRow{
			Provider:  ev.Provider,
			ChannelID: ev.ChannelID,
			Old:       prev,
			New:       h.Status,
			At:        ev.At,
		})
	}
	sort.SliceStable(transitions, func(i, j int) bool {
		return healthSeverity(transitions[i].New) > healthSeverity(transitions[j].New)
	})
	if len(transitions) > healthDeltaCap {
		transitions = transitions[:healthDeltaCap]
	}
	return transitions, nil
}

func healthSeverity(h model.Health) int {
	switch h {
	case model.HealthDown:
		return 3
	case model.HealthDegraded:
		return 2
	case model.HealthHealthy:
		return 1
	default:
		return 0
	}
}

// Rendered is subject + multipart bodies.
type Rendered struct {
	Subject string
	Text    string
	HTML    string
}

func Render(rep Report) Rendered {
	return Rendered{
		Subject: Subject(rep.Kind, rep.LocalDate, rep.Timezone),
		Text:    RenderText(rep),
		HTML:    RenderHTML(rep),
	}
}

func RenderTestStub(baseURL string) Rendered {
	html := renderTestHTML(baseURL)
	text := "GoFAST SMTP test — delivery path is working."
	if baseURL != "" {
		text += "\nStatus: " + strings.TrimRight(baseURL, "/") + "/"
	}
	return Rendered{
		Subject: Subject(KindTest, "", ""),
		Text:    text,
		HTML:    html,
	}
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.UTC().Format(time.RFC3339)
}

func providerStatusLine(p ProviderRow) string {
	if p.LastError != "" {
		return fmt.Sprintf("error: %s", p.LastError)
	}
	if p.FetchedAt.IsZero() {
		return "never fetched"
	}
	return "ok"
}
