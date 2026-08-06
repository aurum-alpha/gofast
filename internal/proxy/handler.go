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
	"github.com/j27-aurum/gofast/internal/provider/distrotv"
)

// Handler serves /stream, /s/{sid}/{n}.m3u8, and /seg/{token}.
type Handler struct {
	Origin   Origin
	Store    *Store
	Reporter *Reporter
	// Distro resolves DistroTV jsrdn opaque catalog URLs at tune-in.
	Distro *distrotv.Resolver
	// PublicBase is the absolute client-facing origin for rewritten playlist
	// URIs (no trailing slash). When set (FASTPROXY_PUBLIC_BASE_URL), it wins
	// over request-derived Host / X-Forwarded-*. Empty keeps local/dev behavior.
	PublicBase string
	playlists  *playlistClient
	segments   *segmentClient
	mint       *mintClient
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
		Distro:    distrotv.NewResolver(nil, "", ""),
		playlists: newPlaylistClient(30 * time.Second),
		segments:  newSegmentClient(),
		mint:      newMintClient(),
	}
}

// Register mounts proxy routes on mux (after /healthz).
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /stream/{provider}/{id}", withCORS(h.serveStream))
	mux.HandleFunc("OPTIONS /stream/{provider}/{id}", corsPreflight)
	mux.HandleFunc("GET /s/{sid}/{n}", withCORS(h.serveSessionMedia))
	mux.HandleFunc("OPTIONS /s/{sid}/{n}", corsPreflight)
	mux.HandleFunc("GET /seg/{token}", withCORS(h.serveSeg))
	mux.HandleFunc("OPTIONS /seg/{token}", corsPreflight)
}

func (h *Handler) serveStream(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	provider := model.ProviderID(r.PathValue("provider"))
	id := strings.TrimSuffix(r.PathValue("id"), ".m3u8")
	clientIP := clientaccess.ClientIP(r)
	ua := truncateUA(r.UserAgent())

	origin, err := h.Origin.Lookup(r.Context(), provider, id)
	if err != nil {
		logEvent(slog.LevelWarn, EventOriginMiss,
			"provider", provider, "channel", id, "client_ip", clientIP, "err", err.Error())
		h.emit(Event{Kind: EventOriginMiss, Provider: string(provider), ChannelID: id, Reason: ReasonOriginMiss, Message: err.Error()})
		http.NotFound(w, r)
		return
	}

	// Dialect branch — see package doc.go and docs/TERMINOLOGY.md.
	// ProxyAmagiRewrite: beacon rewrite + /seg.
	// ProxySessionMint: DAI mint then 302 to stream_manifest (no /seg).
	// ProxyDistroResolve: jsrdn feed resolve then 302 or rewrite.
	// ProxyNone: 302 to catalog upstream (NATIVE / XUMO under proxy_all).
	switch origin.Classification.ProxyKind() {
	case model.ProxySessionMint:
		h.serveSessionMint(w, r, provider, id, origin, clientIP, ua, start)
		return
	case model.ProxyDistroResolve:
		h.serveDistroResolve(w, r, provider, id, origin, clientIP, ua, start)
		return
	case model.ProxyNone:
		// Browser audition (UI hls.js) needs a same-origin rewritten playlist —
		// a 302 to jmp2/Pluto (etc.) fails CORS from localhost. Jellyfin/ffmpeg
		// keep the cheap 302 path (no ?browser=1).
		if r.URL.Query().Get("browser") == "1" {
			h.servePlaylistRewrite(w, r, provider, id, origin, clientIP, ua, start)
			return
		}
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

	h.servePlaylistRewrite(w, r, provider, id, origin, clientIP, ua, start)
}

// servePlaylistRewrite fetches an upstream master/media playlist and rewrites
// URIs through /s and /seg (Amagi SSAI and Distro hosts that need headers).
func (h *Handler) servePlaylistRewrite(w http.ResponseWriter, r *http.Request, provider model.ProviderID, id string, origin ChannelOrigin, clientIP, ua string, start time.Time) {
	publicBase := h.resolvePublicBase(r)
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

// serveSessionMint handles ProxySessionMint: mint (or cache hit) then 302.
func (h *Handler) serveSessionMint(w http.ResponseWriter, r *http.Request, provider model.ProviderID, id string, origin ChannelOrigin, clientIP, ua string, start time.Time) {
	if manifest, ok := h.Store.GetMintedManifest(provider, id); ok {
		logEvent(slog.LevelInfo, EventSessionMint,
			"provider", provider, "channel", id, "client_ip", clientIP, "ua", ua,
			"cached", true, "manifest", urlHostPath(manifest),
			"duration_ms", time.Since(start).Milliseconds())
		h.emit(Event{
			Kind: EventSessionMint, Provider: string(provider), ChannelID: id,
			DurationMS: time.Since(start).Milliseconds(),
			Attrs:      map[string]any{"cached": true, "manifest": urlHostPath(manifest)},
		})
		http.Redirect(w, r, manifest, http.StatusFound)
		return
	}

	eventID, err := daiEventID(origin.StreamURL)
	if err != nil {
		logEvent(slog.LevelWarn, EventSessionMintFail,
			"provider", provider, "channel", id, "reason", ReasonSessionMintBadURL,
			"upstream", urlHostPath(origin.StreamURL), "err", err.Error())
		h.emit(Event{
			Kind: EventSessionMintFail, Provider: string(provider), ChannelID: id,
			Reason: ReasonSessionMintBadURL, Message: err.Error(),
			DurationMS: time.Since(start).Milliseconds(),
		})
		http.Error(w, "session mint: bad catalog URL", http.StatusBadGateway)
		return
	}

	manifest, status, err := h.mint.mint(r.Context(), eventID, origin.RequestHeaders)
	if err != nil {
		reason := classifyUpstreamErr(err, status)
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			reason = ReasonSessionMintAuth
		} else if status == http.StatusNotFound {
			// Dead/disabled DAI asset — not a missing HMAC (auth is 401).
			reason = ReasonSessionMintBadURL
		}
		logEvent(slog.LevelWarn, EventSessionMintFail,
			"provider", provider, "channel", id, "reason", reason,
			"status", status, "event_id", eventID, "err", err.Error())
		h.emit(Event{
			Kind: EventSessionMintFail, Provider: string(provider), ChannelID: id,
			Reason: reason, Status: status, Message: err.Error(),
			DurationMS: time.Since(start).Milliseconds(),
			Attrs:      map[string]any{"event_id": eventID},
		})
		http.Error(w, "session mint failed", http.StatusBadGateway)
		return
	}

	h.Store.PutMintedManifest(provider, id, manifest)
	logEvent(slog.LevelInfo, EventSessionMint,
		"provider", provider, "channel", id, "client_ip", clientIP, "ua", ua,
		"event_id", eventID, "cached", false, "manifest", urlHostPath(manifest),
		"status", status, "duration_ms", time.Since(start).Milliseconds())
	h.emit(Event{
		Kind: EventSessionMint, Provider: string(provider), ChannelID: id,
		Status: status, DurationMS: time.Since(start).Milliseconds(),
		Attrs: map[string]any{
			"event_id": eventID, "cached": false, "manifest": urlHostPath(manifest),
		},
	})
	// Also count as stream_302 for snapshot parity with NATIVE redirects.
	h.emit(Event{
		Kind: EventStream302, Provider: string(provider), ChannelID: id,
		DurationMS: time.Since(start).Milliseconds(),
		Attrs:      map[string]any{"upstream": urlHostPath(manifest), "via": "session_mint"},
	})
	http.Redirect(w, r, manifest, http.StatusFound)
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
	publicBase := h.resolvePublicBase(r)

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
