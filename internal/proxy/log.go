package proxy

import (
	"context"
	"log/slog"
	"strings"
)

// Stable event names shared by slog and gen ingest payloads.
const (
	EventStreamOpen    = "stream_open"
	EventStream302     = "stream_302"
	EventPlaylistOK    = "playlist_ok"
	EventPlaylistFail  = "playlist_fail"
	EventSessionStart  = "session_start"
	EventSegOK         = "seg_ok"
	EventSegFail       = "seg_fail"
	EventSnapshot      = "snapshot"
	EventOriginLookup  = "origin_lookup"
	EventOriginMiss    = "origin_miss"
	EventReportDropped = "report_dropped"
)

// Failure reasons for correlating docker logs with Status rows.
const (
	ReasonOriginMiss      = "origin_miss"
	ReasonUpstream4xx     = "upstream_4xx"
	ReasonUpstream5xx     = "upstream_5xx"
	ReasonUpstreamTimeout = "upstream_timeout"
	ReasonUpstreamError   = "upstream_error"
	ReasonRewriteEmpty    = "rewrite_empty"
	ReasonTokenUnknown    = "token_unknown"
	ReasonClientCancel    = "client_cancel"
)

func logEvent(level slog.Level, event string, attrs ...any) {
	args := make([]any, 0, len(attrs)+2)
	args = append(args, "event", event)
	args = append(args, attrs...)
	slog.Log(context.Background(), level, "fastproxy", args...)
}

func truncateUA(ua string) string {
	const max = 160
	ua = strings.TrimSpace(ua)
	if len(ua) <= max {
		return ua
	}
	return ua[:max] + "…"
}

func urlHostPath(raw string) string {
	if raw == "" {
		return ""
	}
	// Avoid importing net/url in every call site for a display string; keep query out of info logs.
	if i := strings.Index(raw, "://"); i >= 0 {
		rest := raw[i+3:]
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			host := rest[:j]
			path := rest[j:]
			if q := strings.IndexByte(path, '?'); q >= 0 {
				path = path[:q]
			}
			return host + path
		}
		return rest
	}
	if q := strings.IndexByte(raw, '?'); q >= 0 {
		return raw[:q]
	}
	return raw
}
