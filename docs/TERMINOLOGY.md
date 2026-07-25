# GoFAST terminology

Plain-language glossary for streaming jargon used in this repo. Wire values and
package names are in ``backticks``. For architecture and requirements see
[ARCHITECTURE.md](ARCHITECTURE.md) and [SPEC.md](SPEC.md). For how FASTProxy
branches on dialect, see `internal/proxy` package docs (`go doc ./internal/proxy`).

## False friends (read these first)

| People say… | Actually means… |
|-------------|-----------------|
| “SSAI channel” / “beacon channel” | **Not** one playback path. SSAI is a business concept; GoFAST dialects (`AMAGI_SSAI`, `SESSION`, `XUMO_SSAI`) need different handling. |
| “Needs the proxy” | Only **`AMAGI_SSAI`** (rewrite) and **`SESSION`** (mint) require FASTProxy under selective mode. **`XUMO_SSAI` usually does not.** |
| “SESSION = Amagi” | **No.** SESSION is Google DAI mint-on-tune-in. Amagi is extensionless beacon segment URIs. Feeding DistroTV into Amagi rewrite does nothing useful. |
| “Tubi/Plex adapter” | **FASTGen** provider fetchers (lineup + EPG). Not FASTProxy dialect work ([J27-32](https://linear.app/aurum-alpha/issue/J27-32)). |

---

## Formats and protocols

### HLS

**HTTP Live Streaming.** Apple’s adaptive streaming over HTTP. A **master**
playlist lists quality **variants**; each **media** playlist lists **segments**
(short media files, often `.ts`). Players (ffmpeg/Jellyfin) re-fetch playlists
while tuned in.

### M3U / M3U8

Playlist text formats. FASTGen emits **M3U** channel lists for Jellyfin Live TV
(`playlist.m3u`, `{provider}.m3u`) with one URL per channel. Mid-playback, the
player fetches **M3U8** HLS masters/media from upstream or FASTProxy — a
different job from gen’s lineup file.

### XMLTV

XML electronic program guide format. Not a stream dialect. Gen emits
`epg.xml` / `{provider}.xml` so Jellyfin can show Now/Next.

---

## Ads and dialects

### SSAI

**Server-side ad insertion.** Ads were stitched into the stream upstream.
SSAI alone does **not** tell you whether Jellyfin can play the URL or whether
FASTProxy must intervene. See dialect / classification.

### Dialect / classification

GoFAST **playback-path bucket** set at refresh by `internal/classifier`. Wire
values: `NATIVE` | `AMAGI_SSAI` | `SESSION` | `XUMO_SSAI` | `DRM`. Legacy
`BEACON` canonicalizes to `AMAGI_SSAI`. Classification answers “what kind of
stream is this?” — not “is it healthy?” (health is separate).

### `NATIVE`

Plain media segments (or fail-open when classify fetch fails). Gen emits the
upstream URL. FASTProxy not required.

### `AMAGI_SSAI` (legacy `BEACON`)

Amagi-style SSAI: media playlist lines are often **extensionless tracking URLs**
(see beacon). ffmpeg rejects them. **Needs FASTProxy** → beacon rewrite.

Scheduled Health L1 (baseline and retry lane) includes Amagi **only when** an
emitted proxy URL is set (probes go through FASTProxy). Without proxy, Amagi
stays off the L1 timers so we never hit upstream beacons on a schedule.

### Beacon

A tracking URL in an Amagi media playlist. A GET records an ad/impression
touch, then the CDN **redirects** to real segment bytes. FASTProxy still
performs that GET so impressions count; it does not strip ads.

### Beacon rewrite

What FASTProxy does for `AMAGI_SSAI`: fetch upstream playlists, rewrite
player-visible URIs to proxy paths with media-like extensions (e.g.
`/seg/{token}.ts`), then on segment request resolve the beacon and shuttle
bytes with `io.Copy`.

### `SESSION`

**Mint-on-tune-in** dialects. Today: **Google DAI** catalog URLs
(`dai.google.com` + `/linear/hls/…`). Published masters often **404** until a
fresh stream session is created. **Needs FASTProxy** → mint then 302. Not the
Amagi rewrite path.

### Mint / mint-on-tune-in

At the moment the player opens a channel, FASTProxy `POST`s Google’s DAI
stream-create API for the event id, reads JSON, and redirects the player to a
live master. Catalog URLs are labels, not durable playlists.

### Google DAI

Google Ad Manager **Dynamic Ad Insertion**. Linear live events use hosts like
`dai.google.com`. DistroTV published-pair streams commonly classify as
`SESSION`.

### `stream_manifest`

Field in DAI mint JSON: the live HLS master URL the player should use after
mint. (Fallback field name: `hls_master_playlist`.)

### `XUMO_SSAI`

CloudFront / Xumo SSAI that needs **`ads.*` query params** for origin
interpolation. Gen/LG adapter keeps those keys and neutralizes client macros.
**Does not require FASTProxy** — emit upstream and play direct (validated
J27-64; no selective passthrough dialect). Under `proxy_all`, FASTProxy
`ProxyNone` 302s to the full upstream URL so `ads.*` stay on `Location`.

### `ads.*` / macro neutralization

Query keys prefixed `ads.` on catalog stream URLs (e.g. `ads.channelId`).
Bracket macros like `[IFA]` / `[LMT]` are client placeholders; gen empties them
so CloudFront can interpolate. Stripping the whole query breaks Xumo channels.

### `DRM`

Widevine (or similar) with a `license_url`. Never playable in this product; always
dropped from export. Proxy cannot help.

---

## Products and topology

### FASTGen

Primary binary (`fastgen`): providers, classify, emit M3U/XMLTV, health, UI.
Control plane for the lineup.

### FASTProxy

Optional binary (`fastproxy`): dialect translation at tune-in (Amagi rewrite,
SESSION mint). Headless — no `/data`, no product UI; reports into gen.

### Selective proxy vs `proxy_all`

**Selective (default):** gen embeds `{proxy_base_url}/stream/...` only for
dialects that `RequireProxy()` (Amagi + SESSION).

**`proxy_all`:** every exported channel gets that stable `/stream` URL. Proxy
still **rewrites** Amagi, **mints** SESSION, and **302s** NATIVE/XUMO to
upstream. Same M3U URL shape; behavior is decided inside the proxy by
classification.

### `proxy_base_url` vs `proxy_internal_url` vs `FASTPROXY_GEN_URL`

| Knob | Who uses it |
|------|-------------|
| `proxy_base_url` / `FASTGEN_PROXY_BASE_URL` | Jellyfin/browser — embedded in M3U |
| `proxy_internal_url` / `FASTGEN_PROXY_INTERNAL_URL` | Gen health probes rewriting public→Docker DNS |
| `FASTPROXY_GEN_URL` | Proxy → gen (origin lookup + event push) |

Do not conflate these three.

### Origin lookup

FASTProxy asks gen `GET /api/proxy/origin/{provider}/{id}` for `stream_url`,
`classification`, and request headers before handling `/stream`.

### Tune-in

The player starts a channel (opens the emitted stream URL). Mint and Amagi
rewrite run at tune-in, not at gen refresh.

### ffmpeg `allowed_segment_extensions`

ffmpeg security check that rejects segment URIs without a media-like extension.
Why Amagi beacons break Jellyfin without rewrite.

### HEAD vs GET

SSAI endpoints often reject **HEAD** while **GET** works. GoFAST health probes
and FASTProxy never use HEAD against stream endpoints. SESSION mint uses
**POST** (stream create), not HEAD.

---

## Related Linear

- Amagi rewrite: J27-28
- Classifier dialects: J27-49
- Xumo `ads.*` keep: J27-55
- SESSION mint: J27-65
- Xumo validate (no passthrough): J27-64
