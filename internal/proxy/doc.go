// Package proxy implements FASTProxy: the optional dialect-translation add-on
// that sits between Jellyfin/ffmpeg and upstream HLS.
//
// # Why this exists
//
// FASTGen aggregates channels and emits M3U/XMLTV. Most dialects put a normal
// media URL in the playlist and ffmpeg is happy. Some dialects need help at
// tune-in; Jellyfin also needs help for DVR. Those are two orthogonal shapes:
//
//   - Class A — reference legality: playlists list extensionless Amagi
//     “beacon” URIs (or similar). ffmpeg’s allowed_segment_extensions check
//     refuses them. GET /stream/… rewrites player-visible URIs to
//     /seg/{token}.ts, performs the beacon GET, and shuttles bytes.
//   - Class B — elementary-stream constancy: mid-session coded params change
//     (e.g. SSAI ad splice at a different resolution). Live TV may play; DVR
//     -c copy drops video. GET /stable/… re-encodes to a fixed-geometry
//     MPEG-TS pipe so Jellyfin sees one video stream for the whole tune.
//
// Other /stream/ dialect jobs (unchanged):
//
//   - SESSION (Google DAI) — mint-on-tune-in, then 302 to stream_manifest.
//   - STIRR_RESOLVE / DISTRO_RESOLVE — opaque catalog resolve, then 302 or rewrite.
//
// Class A does not imply Class B. /stream/ alone is not a DVR guarantee.
// When gen marks Class B and proxy is configured, EmittedURL is
// {proxy}/stable/{provider}/{id}.ts. If that channel is also Amagi, /stable/
// loopbacks to /stream/ for ingest (A+B) then encodes out.
//
// XUMO_SSAI and NATIVE do not require /stream/ under selective proxying.
// Under proxy_all they 302 unless Class B emission sends them to /stable/
// (Pluto fixture in #56; general detection in #57). DRM is never exported.
//
// Glossary: docs/TERMINOLOGY.md (Class A / Class B, demux-stable, /stable/).
//
// # Control plane
//
// Proxy is headless: no /data mount, no product UI (only /healthz). Gen is the
// control plane. Proxy pulls origin from gen (FASTPROXY_GEN_URL +
// GET /api/proxy/origin/...) and pushes events/snapshots (POST /api/proxy/events)
// including demux_stable_* encode slot telemetry. Optional
// FASTPROXY_PUBLIC_BASE_URL for rewritten playlist URIs behind TLS.
//
// Class B knobs: FASTPROXY_DEMUX_STABLE_MAX (default 2),
// FASTPROXY_DEMUX_STABLE_SIZE (default 1280x720), FASTPROXY_FFMPEG.
//
// # Request flow
//
//   - /stream/{p}/{id}.m3u8 — Class A / dialect (rewrite, mint, resolve, 302)
//   - /stable/{p}/{id}.ts — Class B MPEG-TS pipe (ingest prep then ffmpeg)
//   - /s/{sid}/{n}.m3u8, /seg/{token} — Amagi rewrite session shuttle
package proxy
