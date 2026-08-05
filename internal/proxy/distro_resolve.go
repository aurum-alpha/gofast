package proxy

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/j27-aurum/gofast/internal/classifier"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider/distrotv"
)

const (
	EventDistroResolve     = "distro_resolve"
	EventDistroResolveFail = "distro_resolve_fail"
	ReasonDistroResolve    = "distro_resolve_fail"
)

// serveDistroResolve handles ProxyDistroResolve: jsrdn feed refresh → sanitize
// macros → 302 or playlist rewrite (Amagi / Origin-locked hosts).
func (h *Handler) serveDistroResolve(w http.ResponseWriter, r *http.Request, provider model.ProviderID, id string, origin ChannelOrigin, clientIP, ua string, start time.Time) {
	if h.Distro == nil {
		logEvent(slog.LevelWarn, EventDistroResolveFail,
			"provider", provider, "channel", id, "reason", ReasonDistroResolve,
			"err", "resolver not configured")
		h.emit(Event{
			Kind: EventDistroResolveFail, Provider: string(provider), ChannelID: id,
			Reason: ReasonDistroResolve, Message: "resolver not configured",
			DurationMS: time.Since(start).Milliseconds(),
		})
		http.Error(w, "distro resolve unavailable", http.StatusBadGateway)
		return
	}

	token := origin.StreamURL
	if token == "" {
		token = id
	}
	playURL, err := h.Distro.Resolve(r.Context(), token)
	if err != nil {
		logEvent(slog.LevelWarn, EventDistroResolveFail,
			"provider", provider, "channel", id, "reason", ReasonDistroResolve,
			"err", err.Error())
		h.emit(Event{
			Kind: EventDistroResolveFail, Provider: string(provider), ChannelID: id,
			Reason: ReasonDistroResolve, Message: err.Error(),
			DurationMS: time.Since(start).Milliseconds(),
		})
		http.Error(w, "distro resolve failed", http.StatusBadGateway)
		return
	}

	logEvent(slog.LevelInfo, EventDistroResolve,
		"provider", provider, "channel", id, "client_ip", clientIP, "ua", ua,
		"upstream", urlHostPath(playURL),
		"duration_ms", time.Since(start).Milliseconds())
	h.emit(Event{
		Kind: EventDistroResolve, Provider: string(provider), ChannelID: id,
		DurationMS: time.Since(start).Milliseconds(),
		Attrs:      map[string]any{"upstream": urlHostPath(playURL)},
	})

	resolved := origin
	resolved.StreamURL = playURL
	if class, ok := classifier.FromURL(playURL); ok {
		resolved.Classification = class
	} else {
		resolved.Classification = model.ClassNative
	}

	// Amagi beacons and Origin/Referer / relative-segment hosts need rewrite.
	if resolved.Classification.ProxyKind() == model.ProxyAmagiRewrite || distrotv.NeedsPlaylistProxy(playURL) {
		h.servePlaylistRewrite(w, r, provider, id, resolved, clientIP, ua, start)
		return
	}

	logEvent(slog.LevelInfo, EventStream302,
		"provider", provider, "channel", id, "client_ip", clientIP,
		"ua", ua, "upstream", urlHostPath(playURL),
		"classification", "DISTRO_RESOLVE")
	h.emit(Event{
		Kind: EventStream302, Provider: string(provider), ChannelID: id,
		DurationMS: time.Since(start).Milliseconds(),
		Attrs:      map[string]any{"upstream": urlHostPath(playURL), "via": "distro_resolve"},
	})
	http.Redirect(w, r, playURL, http.StatusFound)
}
