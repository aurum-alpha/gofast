package proxy

import (
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/j27-aurum/gofast/internal/clientaccess"
	"github.com/j27-aurum/gofast/internal/model"
)

// Handler serves /stream, /s/{sid}/{n}.m3u8, and /seg/{token}.
type Handler struct {
	Origin    Origin
	Store     *Store
	Reporter  *Reporter
	playlists *playlistClient
	segments  *segmentClient
}

// NewHandler wires origin lookup, session store, and optional reporter.
func NewHandler(origin Origin, store *Store, reporter *Reporter) *Handler {
	if store == nil {
		store = NewStore()
	}
	return &Handler{
		Origin:    origin,
		Store:     store,
		Reporter:  reporter,
		playlists: newPlaylistClient(30 * time.Second),
		segments:  newSegmentClient(),
	}
}

// Register mounts proxy routes on mux (after /healthz).
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /stream/{provider}/{id}", h.serveStream)
	mux.HandleFunc("GET /s/{sid}/{n}", h.serveSessionMedia)
	mux.HandleFunc("GET /seg/{token}", h.serveSeg)
}

func (h *Handler) serveStream(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	provider := model.ProviderID(r.PathValue("provider"))
	id := strings.TrimSuffix(r.PathValue("id"), ".m3u8")
	clientIP := clientaccess.ClientIP(r)
	ua := truncateUA(r.UserAgent())
	publicBase := publicBaseFromRequest(r)

	origin, err := h.Origin.Lookup(r.Context(), provider, id)
	if err != nil {
		logEvent(slog.LevelWarn, EventOriginMiss,
			"provider", provider, "channel", id, "client_ip", clientIP, "err", err.Error())
		h.emit(Event{Kind: EventOriginMiss, Provider: string(provider), ChannelID: id, Reason: ReasonOriginMiss, Message: err.Error()})
		http.NotFound(w, r)
		return
	}

	if !origin.Classification.RequiresAmagiProxy() {
		logEvent(slog.LevelInfo, EventStream302,
			"provider", provider, "channel", id, "client_ip", clientIP,
			"ua", ua, "upstream", urlHostPath(origin.StreamURL),
			"classification", origin.Classification)
		h.emit(Event{
			Kind: EventStream302, Provider: string(provider), ChannelID: id,
			DurationMS: time.Since(start).Milliseconds(),
			Attrs:      map[string]any{"upstream": urlHostPath(origin.StreamURL)},
		})
		http.Redirect(w, r, origin.StreamURL, http.StatusFound)
		return
	}

	body, finalURL, status, err := h.playlists.get(r.Context(), origin.StreamURL, origin.RequestHeaders)
	if err != nil {
		reason := classifyUpstreamErr(err, status)
		logEvent(slog.LevelWarn, EventPlaylistFail,
			"provider", provider, "channel", id, "reason", reason,
			"status", status, "upstream", urlHostPath(origin.StreamURL), "err", err.Error())
		h.emit(Event{
			Kind: EventPlaylistFail, Provider: string(provider), ChannelID: id,
			Reason: reason, Status: status, Message: err.Error(),
			DurationMS: time.Since(start).Milliseconds(),
		})
		http.Error(w, "upstream playlist fetch failed", http.StatusBadGateway)
		return
	}

	// Mint session first so master rewrite can point variants at /s/{sid}/n.
	sess := h.Store.NewSession(provider, id, finalURL, origin.RequestHeaders, nil)
	rewritten, err := RewritePlaylist(body, finalURL, publicBase, sess.ID, h.Store, origin.RequestHeaders, provider, id)
	if err != nil || rewritten.URIRewrites == 0 && strings.TrimSpace(body) != "" {
		// Empty rewrite on non-empty body is suspicious for Amagi.
		if err == nil && rewritten.URIRewrites == 0 {
			err = errRewriteEmpty
		}
		if err != nil {
			logEvent(slog.LevelWarn, EventPlaylistFail,
				"provider", provider, "channel", id, "reason", ReasonRewriteEmpty, "err", err.Error())
			h.emit(Event{
				Kind: EventPlaylistFail, Provider: string(provider), ChannelID: id,
				Reason: ReasonRewriteEmpty, Message: err.Error(),
			})
			http.Error(w, "playlist rewrite failed", http.StatusBadGateway)
			return
		}
	}
	if len(rewritten.VariantURLs) > 0 {
		sess.VariantURLs = append([]string(nil), rewritten.VariantURLs...)
	}

	logEvent(slog.LevelInfo, EventStreamOpen,
		"provider", provider, "channel", id, "client_ip", clientIP, "ua", ua,
		"public_base", publicBase, "upstream", urlHostPath(finalURL),
		"sid", sess.ID, "is_master", rewritten.IsMaster,
		"uri_rewrites", rewritten.URIRewrites, "status", status,
		"bytes", len(body), "duration_ms", time.Since(start).Milliseconds())
	h.emit(Event{
		Kind: EventStreamOpen, Provider: string(provider), ChannelID: id,
		Status: status, Bytes: int64(len(body)), DurationMS: time.Since(start).Milliseconds(),
		Attrs: map[string]any{
			"sid": sess.ID, "is_master": rewritten.IsMaster, "uri_rewrites": rewritten.URIRewrites,
		},
	})
	logEvent(slog.LevelInfo, EventPlaylistOK,
		"provider", provider, "channel", id, "sid", sess.ID,
		"final_url", urlHostPath(finalURL), "bytes", len(body),
		"uri_rewrites", rewritten.URIRewrites, "is_master", rewritten.IsMaster)
	h.emit(Event{
		Kind: EventPlaylistOK, Provider: string(provider), ChannelID: id,
		Status: status, Bytes: int64(len(body)), DurationMS: time.Since(start).Milliseconds(),
	})

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, rewritten.Body)
}

var errRewriteEmpty = errStringer("rewrite produced no URI changes")

type errStringer string

func (e errStringer) Error() string { return string(e) }

func (h *Handler) serveSessionMedia(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	sid := r.PathValue("sid")
	nStr := strings.TrimSuffix(r.PathValue("n"), ".m3u8")
	n, err := strconv.Atoi(nStr)
	if err != nil || n < 0 {
		http.NotFound(w, r)
		return
	}
	sess, ok := h.Store.GetSession(sid)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if n >= len(sess.VariantURLs) {
		http.NotFound(w, r)
		return
	}
	upstream := sess.VariantURLs[n]
	publicBase := publicBaseFromRequest(r)

	body, finalURL, status, err := h.playlists.get(r.Context(), upstream, sess.RequestHeaders)
	if err != nil {
		reason := classifyUpstreamErr(err, status)
		logEvent(slog.LevelWarn, EventPlaylistFail,
			"sid", sid, "n", n, "provider", sess.Provider, "channel", sess.ChannelID,
			"reason", reason, "status", status, "err", err.Error())
		h.emit(Event{
			Kind: EventPlaylistFail, Provider: string(sess.Provider), ChannelID: sess.ChannelID,
			Reason: reason, Status: status, Message: err.Error(),
			DurationMS: time.Since(start).Milliseconds(),
		})
		http.Error(w, "upstream media playlist fetch failed", http.StatusBadGateway)
		return
	}
	rewritten, err := RewritePlaylist(body, finalURL, publicBase, sid, h.Store, sess.RequestHeaders, sess.Provider, sess.ChannelID)
	if err != nil {
		http.Error(w, "playlist rewrite failed", http.StatusBadGateway)
		return
	}
	logEvent(slog.LevelInfo, EventPlaylistOK,
		"sid", sid, "n", n, "provider", sess.Provider, "channel", sess.ChannelID,
		"bytes", len(body), "uri_rewrites", rewritten.URIRewrites,
		"duration_ms", time.Since(start).Milliseconds())
	h.emit(Event{
		Kind: EventPlaylistOK, Provider: string(sess.Provider), ChannelID: sess.ChannelID,
		Status: status, Bytes: int64(len(body)), DurationMS: time.Since(start).Milliseconds(),
	})
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, rewritten.Body)
}

func (h *Handler) serveSeg(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	token := r.PathValue("token")
	seg, ok := h.Store.GetSeg(token)
	if !ok {
		logEvent(slog.LevelInfo, EventSegFail, "token", token, "reason", ReasonTokenUnknown)
		h.emit(Event{Kind: EventSegFail, Reason: ReasonTokenUnknown, Message: "unknown or expired token"})
		http.NotFound(w, r)
		return
	}

	resp, _, err := h.segments.open(r.Context(), seg.UpstreamURL, seg.RequestHeaders)
	if err != nil {
		reason := classifyUpstreamErr(err, 0)
		if r.Context().Err() != nil {
			reason = ReasonClientCancel
		}
		logEvent(slog.LevelWarn, EventSegFail,
			"token", token, "provider", seg.Provider, "channel", seg.ChannelID,
			"reason", reason, "upstream", urlHostPath(seg.UpstreamURL), "err", err.Error())
		h.emit(Event{
			Kind: EventSegFail, Provider: string(seg.Provider), ChannelID: seg.ChannelID,
			Reason: reason, Message: err.Error(), DurationMS: time.Since(start).Milliseconds(),
		})
		http.Error(w, "segment fetch failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		reason := classifyUpstreamErr(nil, resp.StatusCode)
		logEvent(slog.LevelWarn, EventSegFail,
			"token", token, "provider", seg.Provider, "channel", seg.ChannelID,
			"reason", reason, "status", resp.StatusCode)
		h.emit(Event{
			Kind: EventSegFail, Provider: string(seg.Provider), ChannelID: seg.ChannelID,
			Reason: reason, Status: resp.StatusCode, DurationMS: time.Since(start).Milliseconds(),
		})
		http.Error(w, "segment upstream error", http.StatusBadGateway)
		return
	}

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "video/mp2t")
	}
	w.Header().Set("Cache-Control", "no-store")
	n, copyErr := io.Copy(w, resp.Body)
	if copyErr != nil && r.Context().Err() == nil {
		logEvent(slog.LevelWarn, EventSegFail,
			"token", token, "provider", seg.Provider, "channel", seg.ChannelID,
			"reason", ReasonUpstreamError, "bytes", n, "err", copyErr.Error())
		h.emit(Event{
			Kind: EventSegFail, Provider: string(seg.Provider), ChannelID: seg.ChannelID,
			Reason: ReasonUpstreamError, Message: copyErr.Error(), Bytes: n,
			DurationMS: time.Since(start).Milliseconds(),
		})
		return
	}
	logEvent(slog.LevelInfo, EventSegOK,
		"token", token, "provider", seg.Provider, "channel", seg.ChannelID,
		"upstream", urlHostPath(seg.UpstreamURL), "status", resp.StatusCode,
		"bytes", n, "duration_ms", time.Since(start).Milliseconds())
	h.emit(Event{
		Kind: EventSegOK, Provider: string(seg.Provider), ChannelID: seg.ChannelID,
		Status: resp.StatusCode, Bytes: n, DurationMS: time.Since(start).Milliseconds(),
	})
}

func (h *Handler) emit(ev Event) {
	if h.Reporter != nil {
		h.Reporter.Emit(ev)
	}
}
