package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/j27-aurum/gofast/internal/provider"
)

const metricsContentType = "text/plain; version=0.0.4; charset=utf-8"

type metricsRow struct {
	id                 string
	exportedChannels   int
	exportedProgrammes int
	stale              int
	lastDuration       time.Duration
	successes          uint64
	failures           uint64
	guideHoursAhead    float64
	configuredSecs     float64
	effectiveSecs      float64
}

// MetricsHandler serves GET /metrics in Prometheus text exposition format.
func MetricsHandler(reg *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if reg == nil {
			http.Error(w, "metrics unavailable", http.StatusServiceUnavailable)
			return
		}
		rows := make([]metricsRow, 0, len(reg.Feeds()))
		for _, feed := range reg.Feeds() {
			stats := feed.Stats()
			rm := feed.RefreshMetrics()
			configured, effective, _ := feed.RefreshSchedule()
			stale := 0
			if stats.LastError != "" {
				stale = 1
			}
			rows = append(rows, metricsRow{
				id:                 string(feed.ID()),
				exportedChannels:   stats.ExportedChannels,
				exportedProgrammes: stats.ExportedProgrammes,
				stale:              stale,
				lastDuration:       rm.LastDuration,
				successes:          rm.Successes,
				failures:           rm.Failures,
				guideHoursAhead:    stats.GuideHoursAhead,
				configuredSecs:     configured.Seconds(),
				effectiveSecs:      effective.Seconds(),
			})
		}

		w.Header().Set("Content-Type", metricsContentType)
		var b strings.Builder
		writeGaugeMetric(&b, "gofast_provider_exported_channels",
			"Exported channel count for the provider.", rows, func(row metricsRow) string {
				return strconv.Itoa(row.exportedChannels)
			})
		writeGaugeMetric(&b, "gofast_provider_exported_programmes",
			"Exported programme count for the provider.", rows, func(row metricsRow) string {
				return strconv.Itoa(row.exportedProgrammes)
			})
		writeGaugeMetric(&b, "gofast_provider_stale",
			"1 when the provider has a last refresh error while still serving last-known-good.", rows, func(row metricsRow) string {
				return strconv.Itoa(row.stale)
			})
		writeGaugeMetric(&b, "gofast_provider_refresh_duration_seconds",
			"Wall time of the last completed provider refresh.", rows, func(row metricsRow) string {
				return strconv.FormatFloat(row.lastDuration.Seconds(), 'f', -1, 64)
			})
		writeGaugeMetric(&b, "gofast_provider_guide_hours_ahead",
			"Hours until guide_end for exported programmes (0 if exhausted or unknown).", rows, func(row metricsRow) string {
				return strconv.FormatFloat(row.guideHoursAhead, 'f', -1, 64)
			})
		b.WriteString("# HELP gofast_provider_refresh_total Total provider refresh attempts by result.\n")
		b.WriteString("# TYPE gofast_provider_refresh_total counter\n")
		for _, row := range rows {
			fmt.Fprintf(&b, `gofast_provider_refresh_total{provider=%q,result="success"} %d`+"\n", row.id, row.successes)
			fmt.Fprintf(&b, `gofast_provider_refresh_total{provider=%q,result="failure"} %d`+"\n", row.id, row.failures)
		}
		b.WriteString("# HELP gofast_provider_refresh_interval_seconds Configured and effective provider refresh intervals.\n")
		b.WriteString("# TYPE gofast_provider_refresh_interval_seconds gauge\n")
		for _, row := range rows {
			fmt.Fprintf(&b, `gofast_provider_refresh_interval_seconds{provider=%q,kind="configured"} %s`+"\n",
				row.id, strconv.FormatFloat(row.configuredSecs, 'f', -1, 64))
			fmt.Fprintf(&b, `gofast_provider_refresh_interval_seconds{provider=%q,kind="effective"} %s`+"\n",
				row.id, strconv.FormatFloat(row.effectiveSecs, 'f', -1, 64))
		}
		_, _ = w.Write([]byte(b.String()))
	}
}

func writeGaugeMetric(b *strings.Builder, name, help string, rows []metricsRow, value func(metricsRow) string) {
	b.WriteString("# HELP ")
	b.WriteString(name)
	b.WriteByte(' ')
	b.WriteString(help)
	b.WriteByte('\n')
	b.WriteString("# TYPE ")
	b.WriteString(name)
	b.WriteString(" gauge\n")
	for _, row := range rows {
		fmt.Fprintf(b, `%s{provider=%q} %s`+"\n", name, row.id, value(row))
	}
}
