// Package proxy implements FASTProxy: the optional HLS rewrite add-on that makes
// Amagi SSAI “beacon” channels playable by ffmpeg/Jellyfin.
//
// # Why this exists
//
// FASTGen aggregates channels and emits M3U/XMLTV. Most dialects put a normal
// media URL in the playlist and ffmpeg is happy. Amagi SSAI is different: media
// playlists list extensionless tracking URLs (often containing /beacon/ and
// heavy query strings). A GET on those URLs records an ad/impression touch, then
// the CDN redirects to real segment bytes. ffmpeg’s allowed_segment_extensions
// check refuses the extensionless lines, so Jellyfin cannot play a stream that
// is otherwise correctly classified and often healthy upstream.
//
// FASTProxy’s job is dialect translation, not ad stripping. It fetches upstream
// playlists on behalf of the player, rewrites every player-visible media URI to
// a proxy path with a media-like extension (e.g. /seg/{token}.ts), and when the
// player requests a segment it performs the beacon GET (impressions still count),
// follows redirects, and shuttles bytes with io.Copy. Gen remains the brains of
// the lineup; this package is a network-I/O appliance — loudly instrumented,
// because when playback fails the operator opens gen’s Status glass first.
//
// # Control plane
//
// Proxy is headless: no /data mount, no product UI (only /healthz for container
// liveness). Gen is the control plane. Proxy pulls channel origin from gen
// (FASTPROXY_GEN_URL + GET /api/proxy/origin/...) and asynchronously pushes
// structured events and snapshots to gen (POST /api/proxy/events) so Status can
// show what the proxy is doing without SSH. Media paths never block on ingest.
//
// # Request flow
//
// Gen emits stable URLs of the form {proxy_base_url}/stream/{provider}/{id}.m3u8
// for AMAGI_SSAI (and for all channels when proxy_all is on). On /stream the
// proxy resolves origin, then either rewrites (Amagi) or 302s to upstream
// (NATIVE/SESSION/XUMO under proxy_all). Short-TTL in-memory sessions keep Amagi
// query/session tokens coherent across variant and media playlist polls.
// Segment tokens map opaque /seg/{token}.ts names to absolute upstream URLs.
// HEAD is never used — SSAI endpoints commonly reject it while GET works.
package proxy
