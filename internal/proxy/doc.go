// Package proxy implements FASTProxy: the optional dialect-translation add-on
// that sits between Jellyfin/ffmpeg and upstream HLS.
//
// # Why this exists
//
// FASTGen aggregates channels and emits M3U/XMLTV. Most dialects put a normal
// media URL in the playlist and ffmpeg is happy. Some dialects need help at
// tune-in; they need different help:
//
//   - AMAGI_SSAI — playlists list extensionless tracking (“beacon”) segment
//     URIs. ffmpeg’s allowed_segment_extensions check refuses them. FASTProxy
//     rewrites player-visible URIs to /seg/{token}.ts, performs the beacon GET
//     (impressions still count), follows redirects, and shuttles bytes.
//   - SESSION (Google DAI) — published catalog masters often 404. FASTProxy
//     POSTs DAI stream-create (mint-on-tune-in), then HTTP 302s to the live
//     stream_manifest. No Amagi-style /seg rewrite in v1.
//   - DISTRO_RESOLVE — DistroTV opaque catalog URLs. FASTProxy refreshes
//     Distro’s jsrdn live feed, substitutes macros, then 302s or rewrites.
//
// XUMO_SSAI and NATIVE do not require this package under selective proxying
// (gen emits upstream; J27-64 confirmed no selective passthrough). Under
// proxy_all they hit /stream and get a plain 302 to the full upstream URL
// (ads.* query preserved for Xumo). DRM is never exported.
//
// FASTProxy’s job is dialect translation, not ad stripping. Gen remains the
// brains of the lineup; this package is a network-I/O appliance — loudly
// instrumented, because when playback fails the operator opens gen’s Status /
// Proxy glass first.
//
// Glossary for SSAI / HLS / mint jargon: docs/TERMINOLOGY.md
//
// # Which dialects need us (selective mode)
//
//	NATIVE         — no
//	XUMO_SSAI      — no (keep ads.* at gen; play direct)
//	DRM            — no (drop; proxy cannot help)
//	AMAGI_SSAI     — yes → beacon rewrite + /seg
//	SESSION        — yes → DAI mint → 302 to stream_manifest
//	DISTRO_RESOLVE — yes → jsrdn resolve → 302 or rewrite
//
// # Control plane
//
// Proxy is headless: no /data mount, no product UI (only /healthz for container
// liveness). Gen is the control plane. Proxy pulls channel origin from gen
// (FASTPROXY_GEN_URL + GET /api/proxy/origin/...) and asynchronously pushes
// structured events and snapshots to gen (POST /api/proxy/events) so Status can
// show what the proxy is doing without SSH. Optional FASTPROXY_PUBLIC_BASE_URL
// is the absolute client-facing origin for rewritten playlist URIs (required
// behind TLS-terminating nginx so rewrites are not minted as http://). Media
// paths never block on ingest.
//
// # Request flow
//
// Gen emits stable URLs of the form {proxy_base_url}/stream/{provider}/{id}.m3u8
// for dialects that RequireProxy (Amagi + SESSION + Distro) and for all channels
// when proxy_all is on. On /stream the proxy resolves origin, then branches on
// model.ProxyKind (see serveStream). Short-TTL in-memory state keeps Amagi
// rewrite sessions coherent and caches SESSION mint results briefly. HEAD is
// never used — SSAI endpoints commonly reject it while GET (and mint POST) work.
package proxy
