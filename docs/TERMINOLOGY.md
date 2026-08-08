# GoFAST terminology

Plain-language glossary for streaming jargon used in this repo. Wire values and
package names are in ``backticks``. For architecture and requirements see
[ARCHITECTURE.md](ARCHITECTURE.md) and [SPEC.md](SPEC.md). For how FASTProxy
branches on dialect, see `internal/proxy` package docs (`go doc ./internal/proxy`).

## False friends (read these first)

| People say… | Actually means… |
|-------------|-----------------|
| “SSAI channel” / “beacon channel” | **Not** one playback path. SSAI is a business concept; GoFAST dialects (`AMAGI_SSAI`, `SESSION`, `XUMO_SSAI`) need different handling. |
| “Needs the proxy” | **`AMAGI_SSAI`** (rewrite), **`SESSION`** (DAI mint), **`DISTRO_RESOLVE`** (DistroTV jsrdn), and **`STIRR_RESOLVE`** (STIRR `/playable`) require FASTProxy under selective mode. **`XUMO_SSAI` usually does not.** |
| “SESSION = Amagi” | **No.** SESSION is Google DAI mint-on-tune-in. Amagi is extensionless beacon segment URIs. DistroTV uses **`DISTRO_RESOLVE`**; STIRR uses **`STIRR_RESOLVE`** — neither is SESSION mint. |
| “Tubi/Plex adapter” | **FASTGen** provider fetchers (lineup + EPG). Not FASTProxy dialect work. Plex ships via mjh; Tubi and TCL ship as published-pair. |
| “IPTV” / “paid IPTV” | In Threadfin / m3u-editor / tuliprox land this usually means a **reseller M3U or Xtream panel**, not FAST. GoFAST is **FAST** (free ad-supported catalogs), not that market. |
| “`.strm` for Live TV” | **No.** `.strm` is a **library / VOD** pointer file. Jellyfin Live TV uses **M3U (+ XMLTV)** or an HDHomeRun-style tuner — not `.strm`. |
| “Pluto DVR bug only” | Symptom of **Class B** (demux-unstable ES). Not the same as Amagi **Class A** (illegal segment URIs). See Class A / Class B below. |
| “Amagi rewrite = DVR-safe” | **No.** `/stream/` Class A only renames URIs. Mid-roll resolution changes still break Jellyfin `-c copy`. DVR needs **Class B** `/stable/`. |

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

### `.strm` (Jellyfin / Kodi)

A tiny **text file whose only content is a URL** (sometimes plus a couple of
metadata lines). Jellyfin/Kodi treat it like a local library item: open the
file → play the URL. Tools such as **m3u-editor** / **tuliprox** can export
VOD/series as trees of `.strm` files so movies/shows appear in the **Movies /
TV** library without copying video onto disk.

**Not** how Jellyfin Live TV works. Live TV wants an M3U tuner (or HDHomeRun
emulation) plus XMLTV. Do not confuse “sync `.strm` for series” with “add a
Live TV source.”

### Xtream / Xtream Codes API

A **de facto standard HTTP API** used by many IPTV reseller panels (historical
product name “Xtream Codes”; the panel software evolved, the API shape stuck).
Instead of only giving you a giant M3U URL, the provider gives roughly:

- Panel / server URL (host)
- Username
- Password

Clients then call endpoints like “list live categories,” “list streams,” “EPG,”
“VOD,” “series,” and get short-lived play URLs. Players (TiviMate, IPTV Smarters,
and proxies like tuliprox / m3u-editor) speak this API natively. Many panels also
expose a generated **M3U** that wraps the same account (`…/get.php?username=…`).

**GoFAST does not implement Xtream.** Our providers are FAST APIs / published
M3U+XMLTV pairs / MJH mirrors — not reseller panels.

---

## Ads and dialects

### SSAI

**Server-side ad insertion.** Ads were stitched into the stream upstream.
SSAI alone does **not** tell you whether Jellyfin can play the URL or whether
FASTProxy must intervene. See dialect / classification.

### Dialect / classification

GoFAST **playback-path bucket** set at refresh by `internal/classifier`. Wire
values: `NATIVE` | `AMAGI_SSAI` | `SESSION` | `DISTRO_RESOLVE` | `STIRR_RESOLVE` | `XUMO_SSAI` | `DRM`.
Legacy `BEACON` canonicalizes to `AMAGI_SSAI`. Classification answers “what kind of
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
The same gate applies to **`DISTRO_RESOLVE`** / **`STIRR_RESOLVE`**: L1/L2
probe `EmittedURL` (`/stream/…`), never opaque `distro://` / `stirr://`.

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
`dai.google.com`. Unauthenticated mint against dead Distro published event IDs
returns **404** (not 401) — DistroTV no longer uses this path.

### `DISTRO_RESOLVE`

DistroTV dialect: catalog `StreamURL` is an opaque `distro://channel/{geo}_{id}`
token. At tune-in FASTProxy fetches Distro’s jsrdn live feed, substitutes ad
macros, and **302**s to a fresh HLS URL (or **rewrites** Amagi / Origin-locked
hosts). Not Google DAI mint. Health L1/L2 schedule when `EmittedURL` is set
(probe via proxy resolve path).

### `STIRR_RESOLVE`

STIRR dialect: catalog `StreamURL` is an opaque `stirr://channel/{videoid}`
token. At tune-in FASTProxy `POST`s stirr.com `/api/v2/videos/{id}/playable`,
fills `[vx_nonce]` on the returned master URL, and **302**s (or **rewrites**
Amagi / Origin-locked hosts). Mostly Aniview SSAI after resolve — not Amagi
beacon rewrite and not Google DAI mint. Health L1/L2 schedule when
`EmittedURL` is set (probe via proxy resolve path).

### `stream_manifest`

Field in DAI mint JSON: the live HLS master URL the player should use after
mint. (Fallback field name: `hls_master_playlist`.)

### `XUMO_SSAI`

CloudFront / Xumo SSAI that needs **`ads.*` query params** for origin
interpolation. Gen/LG adapter keeps those keys and neutralizes client macros.
**Does not require FASTProxy** — emit upstream and play direct (validated;
no selective passthrough dialect). Under `proxy_all`, FASTProxy
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

### Logo cache (`cache_logos`)

When enabled, emit rewrites channel logos to `{base_url}/logos/{provider}/{id}.ext`
with **no** boot/refresh download. `GET /logos/…` fills disk lazily (worker pool,
24h conditional revalidate; hard invalidate only when the upstream logo URL
changes). Unused channels never hit the CDN.

### FASTProxy

Optional binary (`fastproxy`): make FAST streams **consumable by ffmpeg/Jellyfin**
(Live TV and DVR). Two URL entry points:

- `{proxy}/stream/{provider}/{id}.m3u8` — Class A / dialect (Amagi rewrite,
  SESSION mint, Distro/STIRR resolve, or NATIVE 302 under `proxy_all`)
- `{proxy}/stable/{provider}/{id}.ts` — Class B demux-stable MPEG-TS pipe
  (ffmpeg re-encode; may Class A–ingest via loopback when dialect needs it)

Headless — no `/data`, no product UI; reports into gen (including
`demux_stable_*` snapshot fields).

### Class A / Class B (Jellyfin compatibility)

Orthogonal shapes — not provider names:

| Class | Jellyfin needs | Failure | Path |
|-------|----------------|---------|------|
| **A — reference legality** | Segment URIs ffmpeg will fetch | Live TV often won’t start | `/stream/` rewrite |
| **B — ES constancy** | One video stream identity for the whole tune | DVR `-c copy` drops video after param change | `/stable/` encode pipe |

**A ⇏ B.** Rewriting names does not stabilize coded resolution.  
**B ⇏ A.** Legal `.ts` URLs can still change dimensions mid-session.  
**Recording** requires Class B on the M3U URL Jellyfin uses. When both A and B
apply, gen emits `/stable/` (not a fourth URL); proxy does A ingest then B out.

Emission matrix (proxy configured): A-only → `/stream/`; B or A+B → `/stable/`;
neither → upstream. Class B without proxy stays soft upstream (#56 Pluto
fixture; #57 general detection).

### Demux-stable (stream)

Class B presentation: ffmpeg’s demuxer keeps a **single video (and audio)
stream identity** for the whole tune-in / recording. Implemented as
`GET /stable/…` → fixed-geometry MPEG-TS (`video/mp2t`).

**Demux-unstable** examples: SSAI stitchers that splice ads at a different coded
resolution than programme content (proven on Pluto). Constant-parameter ladders
(e.g. many Roku channels) stay on the cheap path — no blanket re-encode.

`/stable/` harden (#60): ffmpeg rebuilds filters on size change (`-reinit_filter`,
scale `eval=frame`), requires video+audio maps (no silent audio-only mux), and
**restarts** the encode process on exit/stall while Jellyfin stays connected —
Threadfin-style supervisor, not a Go RAM segment buffer.

GoFAST only applies this to **in-tree FAST providers**, not reseller IPTV.

### Class B packagings: pipe vs HLS facade

The **normalize work** (one ffmpeg per tune: ingest the upstream playlist,
re-encode to fixed geometry / constant ES) is the product. **Packaging** is a
separate choice on top of the same encode:

| | `.ts` pipe (#56, shipped) | HLS facade (#58, planned) |
|---|---|---|
| Shape | One endless `video/mp2t` GET | Real sliding-window `.m3u8` + finite normalized `.ts` segments |
| Consumer | ffmpeg `-f mpegts` (Jellyfin Live TV / DVR) | hls.js browser preview, HLS-only clients |
| Pacing | Receiver TCP backpressure paces the encoder | None — segments must be **complete before listed**, so the encoder runs at full real-time rate regardless of the client |
| “Viewer left” signal | **Definitive**: connection close → immediate `client_cancel`, kill ffmpeg, free slot | **Inferred**: clients only make short polls/GETs; silence for X seconds is the only signal (idle-timeout teardown, always late by the timeout width) |
| Restart | New GET = unambiguous fresh session, join at live | Returning client re-polls the same URL → cold-start ffmpeg, wait out first segment, mind `#EXT-X-MEDIA-SEQUENCE` hygiene |

Key HLS mechanics behind the facade (why it works and what it costs):

- A live media playlist is a **trailing record** of what the encoder has
  finished. It never lists future segments — “listed ⇒ fully downloadable
  right now”. Clients can’t play forward because the future has no URL.
- Clients re-poll the playlist roughly once per target segment duration; each
  refetch slides the window (`#EXT-X-MEDIA-SEQUENCE` advances). The “infinite
  stream” is an emergent effect of finite segments plus polling.
- Segment cadence is **proxy-chosen** (~6 s, keyframes forced on the cut).
  Matching the upstream provider’s segment boundaries was considered and
  rejected: after decode→re-encode the input segmentation no longer exists in
  the output timeline, and Jellyfin never sees the provider playlist, so
  mirroring it is custom PTS/IDR plumbing for zero benefit.
- Minimum glass-to-glass delay is ≥ 1 segment duration (a segment must finish
  before it is listed) — fine for DVR, not a low-latency path.

An `.m3u8` whose single media URI is the infinite pipe is **not** a valid
middle ground: HLS clients wait for the “segment” to end, which it never does.
Extension, body, and protocol shape must agree.

### Selective proxy vs `proxy_all`

**Selective (default):** gen embeds `{proxy_base_url}/stream/...` for dialects
that `RequireProxy()` (Amagi + SESSION + Distro/STIRR), and
`{proxy_base_url}/stable/...ts` for Class B channels (Pluto fixture until #57).

**`proxy_all`:** non–Class-B channels get `/stream` (rewrite / mint / resolve /
302). Class B still wins → `/stable/` when flagged.

### `proxy_base_url` vs `proxy_internal_url` vs proxy envs

| Knob | Who uses it |
|------|-------------|
| `proxy_base_url` / `FASTGEN_PROXY_BASE_URL` | Jellyfin/browser — embedded in M3U |
| `FASTPROXY_PUBLIC_BASE_URL` | Proxy — absolute origin for rewritten `/s` and `/seg` URIs (set behind TLS) |
| `proxy_internal_url` / `FASTGEN_PROXY_INTERNAL_URL` | Gen health probes rewriting public→Docker DNS |
| `FASTPROXY_GEN_URL` | Proxy → gen (origin lookup + event push) |

Do not conflate these. Gen’s `proxy_base_url` and proxy’s `FASTPROXY_PUBLIC_BASE_URL` are usually the same public HTTPS origin.

### Origin lookup

FASTProxy asks gen `GET /api/proxy/origin/{provider}/{id}` for `stream_url`,
`classification`, and request headers before handling `/stream`.

### Tune-in

The player starts a channel (opens the emitted stream URL). Mint and Amagi
rewrite run at tune-in, not at gen refresh.

### ffmpeg `allowed_segment_extensions`

ffmpeg security check that rejects segment URIs without a media-like extension.
Why Amagi beacons break Jellyfin without rewrite. Jellyfin 10.11.x / jellyfin-ffmpeg
7.1+ hit this as playback failure (ffmpeg exit 234) and will not pass
`-allowed_segment_extensions ALL` for operators —
[jellyfin#17400](https://github.com/jellyfin/jellyfin/issues/17400).

### HEAD vs GET

SSAI endpoints often reject **HEAD** while **GET** works. GoFAST health probes
and FASTProxy never use HEAD against stream endpoints. SESSION mint uses
**POST** (stream create), not HEAD.

---

## Adjacent IPTV ecosystem (not GoFAST)

Terms you will hit when reading Threadfin / m3u-editor / tuliprox docs. These
products solve a **different** problem (broker someone else’s M3U/Xtream into
Plex/Jellyfin). GoFAST fetches **FAST** catalogs and fixes **stream dialects**.

### Paid IPTV M3U

A **subscription playlist** sold as credentials: usually an **M3U URL** and/or
**Xtream** host+user+pass that unlocks thousands of live (and often VOD)
channels. The file looks like any other M3U (`#EXTINF` + stream URL per
channel); the difference is **who hosts the streams** and **how you got access**.

| Kind | What it is | Examples / how you get it |
|------|------------|---------------------------|
| **FAST / free AVOD** (GoFAST’s world) | Ad-supported apps with public or community-scraped lineups | Pluto, Samsung TV Plus, Roku, LG Channels, Tubi, STIRR — via GoFAST adapters, not a purchased panel |
| **Legal pay TV apps** | Official apps / devices; rarely hand you a raw M3U | YouTube TV, Hulu + Live, Fubo, cable apps — **no** generic M3U for Jellyfin |
| **Tuner / antenna** | Local RF → network tuner | HDHomeRun + antenna; Jellyfin talks HDHomeRun natively (no “IPTV M3U”) |
| **Reseller “IPTV” panels** | Third-party sellers of M3U/Xtream credentials | Sold on forums, Telegram, “IPTV subscription” shops as Xtream host/user/pass or a `get.php` M3U URL |

That last row is what Threadfin / m3u-editor / tuliprox are usually built around:
you **buy or are given** panel credentials, paste them into the proxy, map
channels, point Jellyfin/Plex at the proxy’s M3U or HDHomeRun endpoint.

**Legal note:** many reseller panels redistribute copyrighted linear TV without
rights. GoFAST does not sell, list, or integrate those panels. Prefer FAST
sources, legal apps, or a tuner you own. If you already have a legitimate
provider that *officially* gives M3U/Xtream, the same tools can broker it —
the format is not illegal; the content rights are the issue.

### Threadfin / xTeVe-style DVR buffering

**Not** Jellyfin’s DVR feature by itself. It means: sit a proxy (classic
**xTeVe**, or its maintained fork **[Threadfin](https://github.com/Threadfin/Threadfin)**)
between the M3U and the media server, and on tune-in (or continuously) **pull
the upstream HLS through ffmpeg/VLC**, then hand Jellyfin/Plex a **stable
re-streamed pipe** (often MPEG-TS or rewritten HLS).

Why people do it:

- Media servers see one continuous stream instead of a flaky multi-variant HLS.
- Fixes some **SSAI splice** failures (e.g. Pluto mid-roll resolution changes
  that break ffmpeg `copy` DVR — see GitHub issue #56).
- Emulates **HDHomeRun** tuners so Plex DVR “just works.”

Cost: CPU/RAM on the proxy host; concurrent “tuners” are limited. GoFAST does
not become a general IPTV buffer for arbitrary M3Us. **#56** is the FAST-scoped
equivalent: demux-stable output from FASTProxy for any in-tree provider that
needs it (Pluto is the first fixture), same compatibility mission as Amagi/DAI
resolve — not “re-encode Pluto only” and not paid panels.

### Threadfin (product)

Go M3U proxy descended from abandoned **xTeVe**. Merge playlists, map channels,
optional ffmpeg/VLC buffer, HDHomeRun-style output for Plex/Jellyfin/Emby.
**Does not** fetch FAST providers; you feed it an M3U (e.g. GoFAST’s
`playlist.m3u`, or a paid panel M3U).

### m3u-editor / tuliprox (products)

Broader IPTV editors/gateways (see their repos). Same niche as Threadfin —
ingest M3U/Xtream, edit/filter, proxy or export (including `.strm` / Xtream API
/ HDHomeRun) — not FAST provider adapters.
