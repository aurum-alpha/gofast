package proxy

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/j27-aurum/gofast/internal/classifier"
	"github.com/j27-aurum/gofast/internal/model"
	"github.com/j27-aurum/gofast/internal/provider/distrotv"
	"github.com/j27-aurum/gofast/internal/provider/stirr"
)

const (
	EventStirrResolve     = "stirr_resolve"
	EventStirrResolveFail = "stirr_resolve_fail"
	ReasonStirrResolve    = "stirr_resolve_fail"
	ReasonStirrDeadSSAI   = "stirr_dead_ssai"
)

// serveStirrResolve handles ProxyStirrResolve: POST /playable → fill macros →
// probe Aniview CON → 302 or playlist rewrite (Amagi / Origin-locked / browser).
func (h *Handler) serveStirrResolve(w http.ResponseWriter, r *http.Request, provider model.ProviderID, id string, origin ChannelOrigin, clientIP, ua string, start time.Time) {
	if h.Stirr == nil {
		logEvent(slog.LevelWarn, EventStirrResolveFail,
			"provider", provider, "channel", id, "reason", ReasonStirrResolve,
			"err", "resolver not configured")
		h.emit(Event{
			Kind: EventStirrResolveFail, Provider: string(provider), ChannelID: id,
			Reason: ReasonStirrResolve, Message: "resolver not configured",
			Status: http.StatusBadGateway, DurationMS: time.Since(start).Milliseconds(),
		})
		http.Error(w, "stirr resolve unavailable", http.StatusBadGateway)
		return
	}

	token := origin.StreamURL
	if token == "" {
		token = id
	}
	playURL, err := h.Stirr.Resolve(r.Context(), token)
	if err != nil {
		reason := ReasonStirrResolve
		msg := err.Error()
		if errors.Is(err, stirr.ErrDeadSSAI) {
			reason = ReasonStirrDeadSSAI
			msg = "Aniview SSAI config deleted (CON) — channel listed but not playable"
		}
		logEvent(slog.LevelWarn, EventStirrResolveFail,
			"provider", provider, "channel", id, "reason", reason,
			"err", msg)
		h.emit(Event{
			Kind: EventStirrResolveFail, Provider: string(provider), ChannelID: id,
			Reason: reason, Message: msg,
			Status: http.StatusBadGateway, DurationMS: time.Since(start).Milliseconds(),
		})
		http.Error(w, "stirr resolve failed", http.StatusBadGateway)
		return
	}

	logEvent(slog.LevelInfo, EventStirrResolve,
		"provider", provider, "channel", id, "client_ip", clientIP, "ua", ua,
		"upstream", urlHostPath(playURL),
		"duration_ms", time.Since(start).Milliseconds())
	h.emit(Event{
		Kind: EventStirrResolve, Provider: string(provider), ChannelID: id,
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

	// Browser audition needs a same-origin rewritten playlist (Aniview has no CORS).
	browser := r.URL.Query().Get("browser") == "1"
	if browser || resolved.Classification.ProxyKind() == model.ProxyAmagiRewrite || distrotv.NeedsPlaylistProxy(playURL) {
		h.servePlaylistRewrite(w, r, provider, id, resolved, clientIP, ua, start)
		return
	}

	logEvent(slog.LevelInfo, EventStream302,
		"provider", provider, "channel", id, "client_ip", clientIP,
		"ua", ua, "upstream", urlHostPath(playURL),
		"classification", "STIRR_RESOLVE")
	h.emit(Event{
		Kind: EventStream302, Provider: string(provider), ChannelID: id,
		DurationMS: time.Since(start).Milliseconds(),
		Attrs:      map[string]any{"upstream": urlHostPath(playURL), "via": "stirr_resolve"},
	})
	http.Redirect(w, r, playURL, http.StatusFound)
}
