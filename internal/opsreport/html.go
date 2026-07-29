package opsreport

import (
	"bytes"
	"fmt"
	"html"
	"strings"
)

// Colors copied from web/src/index.css (:root) and badge/stat patterns in App.css.
const (
	colorInk    = "#e8ebef"
	colorPaper  = "#101014"
	colorPanel  = "#1a1c22"
	colorHeader = "#232631"
	colorAccent = "#00a4dc"
	colorMuted  = "#9aa0ac"
	colorLine   = "#33363f"
	colorShadow = "0 8px 24px rgba(0, 0, 0, 0.45)"
	fontStack   = "'Segoe UI','Helvetica Neue',Arial,sans-serif"
	// Status pills match .badge / .badge-native / .badge-beacon / .badge-drm
	badgeHealthyBG  = "#d8f0c8"
	badgeHealthyBD  = "#9cbc7a"
	badgeDegradedBG = "#fff3c4"
	badgeDegradedBD = "#d4b84a"
	badgeDownBG     = "#f0d0d0"
	badgeDownBD     = "#c48080"
	badgeText       = "#111111"
	colorWarn       = "#8f4a52"
	colorAdded      = "#d8f0c8"
	colorDropped    = "#f0d0d0"
)

// RenderHTML builds a rich, email-safe HTML body matching the operator UI dark theme.
func RenderHTML(rep Report) string {
	var b bytes.Buffer
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><meta name="color-scheme" content="dark"><title>`)
	b.WriteString(html.EscapeString(Subject(rep.Kind, rep.LocalDate, rep.Timezone)))
	b.WriteString(`</title></head><body style="margin:0;padding:0;background:linear-gradient(180deg,#101014 0%,#16181f 100%);color:`)
	b.WriteString(colorInk)
	b.WriteString(`;font-family:`)
	b.WriteString(fontStack)
	b.WriteString(`;">`)
	b.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:`)
	b.WriteString(colorPaper)
	b.WriteString(`;"><tr><td align="center" style="padding:24px 12px;">`)
	b.WriteString(`<table role="presentation" width="640" cellpadding="0" cellspacing="0" style="max-width:640px;width:100%;background:`)
	b.WriteString(colorPanel)
	b.WriteString(`;border:1px solid `)
	b.WriteString(colorLine)
	b.WriteString(`;box-shadow:`)
	b.WriteString(colorShadow)
	b.WriteString(`;">`)

	// Header — matches .top / .brand
	b.WriteString(`<tr><td style="padding:20px 24px;border-bottom:2px solid `)
	b.WriteString(colorInk)
	b.WriteString(`;background:`)
	b.WriteString(colorHeader)
	b.WriteString(`;">`)
	b.WriteString(`<div style="font-family:`)
	b.WriteString(fontStack)
	b.WriteString(`;font-size:28px;font-weight:700;letter-spacing:-0.04em;line-height:1;color:`)
	b.WriteString(colorInk)
	b.WriteString(`;">GoFAST</div>`)
	title := "Daily ops report"
	if rep.Kind == KindPreview {
		title = "Preview ops report"
	}
	b.WriteString(`<div style="margin-top:8px;font-size:16px;font-weight:600;color:`)
	b.WriteString(colorAccent)
	b.WriteString(`;">`)
	b.WriteString(html.EscapeString(title))
	b.WriteString(`</div>`)
	b.WriteString(`<div style="margin-top:8px;font-size:13px;color:`)
	b.WriteString(colorMuted)
	b.WriteString(`;line-height:1.45;">`)
	b.WriteString(html.EscapeString(fmt.Sprintf("%s · %s → %s UTC · zone %s",
		rep.LocalDate, fmtTime(rep.WindowStart), fmtTime(rep.WindowEnd), rep.Timezone)))
	b.WriteString(`</div></td></tr>`)

	b.WriteString(`<tr><td style="padding:20px 24px;background:`)
	b.WriteString(colorPanel)
	b.WriteString(`;color:`)
	b.WriteString(colorInk)
	b.WriteString(`;">`)

	// Health metrics — .stat-grid / .stat
	writeSectionStart(&b, "Fleet health")
	b.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr>`)
	writeMetric(&b, "Healthy", fmt.Sprintf("%d", rep.Health.Healthy), colorInk, false)
	writeMetric(&b, "Degraded", fmt.Sprintf("%d", rep.Health.Degraded), colorWarn, rep.Health.Degraded > 0)
	writeMetric(&b, "Down", fmt.Sprintf("%d", rep.Health.Down), colorWarn, rep.Health.Down > 0)
	writeMetric(&b, "Untested", fmt.Sprintf("%d", rep.Health.Untested), colorInk, false)
	b.WriteString(`</tr></table>`)
	if len(rep.Health.Worst) > 0 {
		b.WriteString(`<div style="margin-top:12px;font-size:12px;text-transform:uppercase;letter-spacing:0.05em;color:`)
		b.WriteString(colorMuted)
		b.WriteString(`;margin-bottom:6px;">Worst channels</div>`)
		writeTableStart(&b, []string{"Provider", "Channel", "Status"})
		for _, w := range rep.Health.Worst {
			writeTableRow(&b, []string{string(w.Provider), displayName(w.Name, w.ChannelID), string(w.Status)}, string(w.Status))
		}
		writeTableEnd(&b)
	}
	writeSectionEnd(&b)

	// Providers
	writeSectionStart(&b, "Providers")
	if len(rep.Providers) == 0 {
		writeEmpty(&b, "No providers configured.")
	} else {
		writeTableStart(&b, []string{"Provider", "Status", "Exported", "Refresh OK/Fail", "Guide h"})
		for _, p := range rep.Providers {
			en := "off"
			if p.Enabled {
				en = providerStatusLine(p)
			}
			writeTableRow(&b, []string{
				fmt.Sprintf("%s (%s)", p.Label, p.ID),
				en,
				fmt.Sprintf("%d", p.Exported),
				fmt.Sprintf("%d / %d", p.RefreshOK, p.RefreshFail),
				fmt.Sprintf("%.1f", p.GuideHoursAhead),
			}, "")
		}
		writeTableEnd(&b)
	}
	writeSectionEnd(&b)

	// Deltas
	writeSectionStart(&b, "Channel deltas")
	writeDeltaBlock(&b, "Added", colorAdded, deltaNames(rep.Added))
	writeDeltaBlock(&b, "Dropped", colorDropped, deltaNames(rep.Dropped))
	if len(rep.ClassChanges) == 0 {
		writeDeltaBlock(&b, "Classification changes", colorAccent, nil)
	} else {
		b.WriteString(`<div style="margin:12px 0 6px;font-size:14px;font-weight:600;color:`)
		b.WriteString(colorInk)
		b.WriteString(`;">Classification changes <span style="color:`)
		b.WriteString(colorMuted)
		b.WriteString(`;font-weight:400;">(`)
		b.WriteString(fmt.Sprintf("%d", len(rep.ClassChanges)))
		b.WriteString(`)</span></div>`)
		writeTableStart(&b, []string{"Provider", "Channel", "Change"})
		for _, d := range rep.ClassChanges {
			writeTableRow(&b, []string{
				string(d.Provider),
				displayName(d.Name, d.ChannelID),
				fmt.Sprintf("%s → %s", emptyDash(d.Old), emptyDash(d.New)),
			}, "")
		}
		writeTableEnd(&b)
	}
	if len(rep.HealthChanges) == 0 {
		writeDeltaBlock(&b, "Health transitions", colorAccent, nil)
	} else {
		b.WriteString(`<div style="margin:12px 0 6px;font-size:14px;font-weight:600;color:`)
		b.WriteString(colorInk)
		b.WriteString(`;">Health transitions <span style="color:`)
		b.WriteString(colorMuted)
		b.WriteString(`;font-weight:400;">(`)
		b.WriteString(fmt.Sprintf("%d", len(rep.HealthChanges)))
		b.WriteString(`)</span></div>`)
		writeTableStart(&b, []string{"Provider", "Channel", "Change"})
		for _, d := range rep.HealthChanges {
			writeTableRow(&b, []string{
				string(d.Provider),
				displayName(d.Name, d.ChannelID),
				fmt.Sprintf("%s → %s", d.Old, d.New),
			}, string(d.New))
		}
		writeTableEnd(&b)
	}
	writeSectionEnd(&b)

	if rep.BaseURL != "" {
		b.WriteString(`<div style="margin-top:20px;text-align:center;">`)
		b.WriteString(`<a href="`)
		b.WriteString(html.EscapeString(rep.BaseURL + "/"))
		b.WriteString(`" style="display:inline-block;padding:10px 18px;background:`)
		b.WriteString(colorAccent)
		b.WriteString(`;color:#101014;text-decoration:none;font-weight:700;letter-spacing:0.02em;">Open Status</a></div>`)
	}

	b.WriteString(`</td></tr></table></td></tr></table></body></html>`)
	return b.String()
}

func renderTestHTML(baseURL string) string {
	var b bytes.Buffer
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="color-scheme" content="dark"></head>`)
	b.WriteString(`<body style="margin:0;padding:24px;background:linear-gradient(180deg,#101014 0%,#16181f 100%);font-family:`)
	b.WriteString(fontStack)
	b.WriteString(`;color:`)
	b.WriteString(colorInk)
	b.WriteString(`;">`)
	b.WriteString(`<div style="max-width:480px;margin:0 auto;padding:24px;background:`)
	b.WriteString(colorPanel)
	b.WriteString(`;border:1px solid `)
	b.WriteString(colorLine)
	b.WriteString(`;border-top:2px solid `)
	b.WriteString(colorInk)
	b.WriteString(`;box-shadow:`)
	b.WriteString(colorShadow)
	b.WriteString(`;">`)
	b.WriteString(`<div style="font-size:28px;font-weight:700;letter-spacing:-0.04em;color:`)
	b.WriteString(colorInk)
	b.WriteString(`;">GoFAST</div>`)
	b.WriteString(`<p style="color:`)
	b.WriteString(colorAccent)
	b.WriteString(`;font-weight:600;margin:8px 0;">SMTP test</p>`)
	b.WriteString(`<p style="color:`)
	b.WriteString(colorMuted)
	b.WriteString(`;line-height:1.45;">Delivery path is working. This message is not archived.</p>`)
	if baseURL != "" {
		b.WriteString(`<p><a href="`)
		b.WriteString(html.EscapeString(strings.TrimRight(baseURL, "/") + "/"))
		b.WriteString(`" style="color:`)
		b.WriteString(colorAccent)
		b.WriteString(`;font-weight:600;text-decoration:none;">Open Status</a></p>`)
	}
	b.WriteString(`</div></body></html>`)
	return b.String()
}

func writeSectionStart(b *bytes.Buffer, title string) {
	b.WriteString(`<div style="margin:0 0 20px;">`)
	b.WriteString(`<div style="font-size:15px;font-weight:700;letter-spacing:-0.02em;margin:0 0 10px;padding-bottom:6px;border-bottom:2px solid `)
	b.WriteString(colorInk)
	b.WriteString(`;color:`)
	b.WriteString(colorInk)
	b.WriteString(`;">`)
	b.WriteString(html.EscapeString(title))
	b.WriteString(`</div>`)
}

func writeSectionEnd(b *bytes.Buffer) {
	b.WriteString(`</div>`)
}

func writeMetric(b *bytes.Buffer, label, value, valueColor string, warn bool) {
	border := colorLine
	if warn {
		border = colorWarn
	}
	b.WriteString(`<td width="25%" style="padding:6px;vertical-align:top;">`)
	b.WriteString(`<div style="background:`)
	b.WriteString(colorPanel)
	b.WriteString(`;border:1px solid `)
	b.WriteString(border)
	b.WriteString(`;box-shadow:`)
	b.WriteString(colorShadow)
	b.WriteString(`;padding:12px;">`)
	b.WriteString(`<div style="font-size:12px;text-transform:uppercase;letter-spacing:0.05em;color:`)
	b.WriteString(colorMuted)
	b.WriteString(`;margin-bottom:6px;">`)
	b.WriteString(html.EscapeString(label))
	b.WriteString(`</div><div style="font-size:22px;font-weight:700;color:`)
	b.WriteString(valueColor)
	b.WriteString(`;">`)
	b.WriteString(html.EscapeString(value))
	b.WriteString(`</div></div></td>`)
}

func writeTableStart(b *bytes.Buffer, headers []string) {
	b.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="border-collapse:collapse;font-size:13px;color:`)
	b.WriteString(colorInk)
	b.WriteString(`;">`)
	b.WriteString(`<tr>`)
	for _, h := range headers {
		b.WriteString(`<th align="left" style="padding:8px 6px;border-bottom:2px solid `)
		b.WriteString(colorLine)
		b.WriteString(`;color:`)
		b.WriteString(colorMuted)
		b.WriteString(`;font-weight:600;text-transform:uppercase;letter-spacing:0.04em;font-size:11px;">`)
		b.WriteString(html.EscapeString(h))
		b.WriteString(`</th>`)
	}
	b.WriteString(`</tr>`)
}

func writeTableRow(b *bytes.Buffer, cols []string, statusKind string) {
	b.WriteString(`<tr>`)
	for i, c := range cols {
		b.WriteString(`<td style="padding:8px 6px;border-bottom:1px solid `)
		b.WriteString(colorLine)
		b.WriteString(`;color:`)
		b.WriteString(colorInk)
		b.WriteString(`;">`)
		if i == len(cols)-1 && statusKind != "" {
			bg, bd := badgeStyle(statusKind)
			b.WriteString(`<span style="display:inline-block;padding:2px 8px;border:1px solid `)
			b.WriteString(bd)
			b.WriteString(`;background:`)
			b.WriteString(bg)
			b.WriteString(`;color:`)
			b.WriteString(badgeText)
			b.WriteString(`;font-size:12px;font-weight:700;letter-spacing:0.04em;">`)
			b.WriteString(html.EscapeString(c))
			b.WriteString(`</span>`)
		} else {
			b.WriteString(html.EscapeString(c))
		}
		b.WriteString(`</td>`)
	}
	b.WriteString(`</tr>`)
}

func writeTableEnd(b *bytes.Buffer) {
	b.WriteString(`</table>`)
}

func writeEmpty(b *bytes.Buffer, msg string) {
	b.WriteString(`<div style="font-size:13px;color:`)
	b.WriteString(colorMuted)
	b.WriteString(`;font-style:italic;">`)
	b.WriteString(html.EscapeString(msg))
	b.WriteString(`</div>`)
}

func writeDeltaBlock(b *bytes.Buffer, title, accent string, lines []string) {
	b.WriteString(`<div style="margin:12px 0 6px;font-size:14px;font-weight:600;color:`)
	b.WriteString(colorInk)
	b.WriteString(`;">`)
	b.WriteString(html.EscapeString(title))
	b.WriteString(` <span style="color:`)
	b.WriteString(colorMuted)
	b.WriteString(`;font-weight:400;">(`)
	b.WriteString(fmt.Sprintf("%d", len(lines)))
	b.WriteString(`)</span></div>`)
	if len(lines) == 0 {
		writeEmpty(b, "none in window")
		return
	}
	b.WriteString(`<ul style="margin:0;padding-left:18px;font-size:13px;color:`)
	b.WriteString(colorInk)
	b.WriteString(`;">`)
	for _, line := range lines {
		b.WriteString(`<li style="margin:4px 0;">`)
		if accent == colorAdded || accent == colorDropped {
			bg, bd := accent, colorLine
			if accent == colorAdded {
				bd = badgeHealthyBD
			} else {
				bd = badgeDownBD
			}
			b.WriteString(`<span style="display:inline-block;padding:1px 6px;margin-right:6px;border:1px solid `)
			b.WriteString(bd)
			b.WriteString(`;background:`)
			b.WriteString(bg)
			b.WriteString(`;color:`)
			b.WriteString(badgeText)
			b.WriteString(`;font-size:11px;font-weight:700;">•</span>`)
		}
		b.WriteString(html.EscapeString(line))
		b.WriteString(`</li>`)
	}
	b.WriteString(`</ul>`)
}

func deltaNames(rows []DeltaRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, fmt.Sprintf("%s · %s (%s)", r.Provider, displayName(r.Name, r.ChannelID), r.ChannelID))
	}
	return out
}

func displayName(name, id string) string {
	if name != "" {
		return name
	}
	return id
}

func emptyDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func badgeStyle(status string) (bg, border string) {
	switch status {
	case "healthy":
		return badgeHealthyBG, badgeHealthyBD
	case "degraded":
		return badgeDegradedBG, badgeDegradedBD
	case "down":
		return badgeDownBG, badgeDownBD
	default:
		return colorHeader, colorLine
	}
}
