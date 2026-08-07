# SPEC: FASTGen + FASTProxy — self-hosted FAST channel aggregator for Jellyfin

Build a production-quality Go project delivering two cooperating services from
one module and **two binaries** (one Docker image each), for Docker Compose
deployment. Domain knowledge below is hard-won from debugging real failures —
treat every "gotcha" as a requirement.

## Architecture (two services, shared core)

One Go module. Two entrypoints — **no mode flags**:

- `cmd/fastgen` → image `fastgen` — **primary product**
- `cmd/fastproxy` → image `fastproxy` — **optional add-on**

Shared `internal/` packages: provider adapters, channel model, config, HTTP
plumbing, and the **stream classifier** (see below), which both services
depend on. Do not ship a single binary with `--enable-gen` / `--enable-proxy`.

**FASTGen** (metadata service): background refresh loops pull channel lineups and
EPG from providers, normalize, filter, cache, and serve M3U + XMLTV over HTTP.
Hosts the embedded UI. Fully functional standalone — proxy integration is
optional config (`proxy_base_url`).

**FASTProxy** (HLS rewriting proxy): makes Amagi-SSAI "beacon" channels playable
by ffmpeg/Jellyfin. Fetches the upstream playlist per request, rewrites variant
and segment URLs to itself, resolves beacon redirects server-side, streams the
segment bytes through. Holds short-TTL per-session state for upstream session
tokens. Probes upstreams with ranged GETs, never HEAD (SSAI endpoints reject
HEAD with 404/405 while GET works).

**The classifier** probes each channel at refresh time and buckets it by
**stream dialect** (playback path), not health. Plain-language glossary:
[`docs/TERMINOLOGY.md`](TERMINOLOGY.md).

- `NATIVE`: plain media segments — plays anywhere (also fail-open on fetch error)
- `AMAGI_SSAI` (legacy wire `BEACON`): Amagi SSAI — segment lines are
  extensionless tracking URLs (`/beacon/`, query-laden, no `.ts`) that ffmpeg's
  `allowed_segment_extensions` security check rejects (ffmpeg 7.1+; Jellyfin
  10.11.x surfaces this as ffmpeg exit 234 and does not expose override flags —
  see [jellyfin#17400](https://github.com/jellyfin/jellyfin/issues/17400));
  **FASTProxy** rewrites those URIs to `/seg/{token}.ts` so Jellyfin/ffmpeg can play
- `SESSION`: mint-on-tune-in dialects (Google DAI today: `dai.google.com` +
  `/linear/hls/…`). Static published masters often 404 without a fresh session;
  **FASTProxy** POSTs DAI stream-create then HTTP 302s to `stream_manifest`
  (not the Amagi rewrite path)
- `DISTRO_RESOLVE`: DistroTV jsrdn opaque catalog URLs; **FASTProxy** refreshes
  the live feed at tune-in, substitutes macros, then 302s or rewrites
- `STIRR_RESOLVE`: STIRR opaque catalog URLs (`stirr://channel/{id}`);
  **FASTProxy** POSTs `/api/v2/videos/{id}/playable`, fills `[vx_nonce]`, then
  302s or rewrites. Mostly Aniview SSAI after resolve (not Amagi).
- `XUMO_SSAI`: CloudFront/Xumo SSAI needing `ads.*` query params for origin
  interpolation; emit upstream URL as-is (keep query); **not** Amagi rewrite
  and **not** required to go through FASTProxy. Future: AWS MediaTailor and
  similar stay out of this bucket until we have a detection signal.
- `DRM`: Widevine `license_url` — never playable, no proxy can help

**Emission decision table** (in FASTGen):
- `NATIVE` → emit upstream URL directly (no proxy in media path)
- `DRM` → drop always
- `AMAGI_SSAI` → if `proxy_base_url` configured, emit
  `{proxy_base_url}/stream/{provider}/{id}.m3u8`; else drop with a logged count
- `SESSION` → same as Amagi: emit proxy `/stream/...` when `proxy_base_url`
  is set; else drop (“needs FASTProxy”). Proxy mints then 302s — do **not**
  use Amagi rewrite for SESSION
- `DISTRO_RESOLVE` → same emission as SESSION (proxy `/stream/...`); proxy
  resolves jsrdn then 302/rewrite — do **not** use Google DAI mint
- `STIRR_RESOLVE` → same emission as DISTRO_RESOLVE (proxy `/stream/...`);
  proxy POSTs playable then 302/rewrite
- `XUMO_SSAI` → emit upstream URL like NATIVE (do **not** route through Amagi
  rewrite). No selective passthrough dialect — play direct with
  `ads.*` retained; under `proxy_all` the proxy 302 keeps the full query on
  `Location`.

**Deployment topologies:** gen-only (default compose) or gen + proxy (compose
`--profile proxy` or equivalent). When proxy is used, `proxy_base_url` in the
gen config must be the proxy address **AS REACHABLE BY JELLYFIN/FFMPEG**, since
it is embedded in M3U output and resolved by media clients, not by FASTGen.
There is no combined single-process mode. Sizing note: gen is a handful of HTTP
pulls and XML marshaling a few times daily (negligible); proxy is
network-I/O-bound byte shuttling (goroutine-per-connection + io.Copy;
bandwidth-limited, not CPU-limited).

**proxy_all mode (optional, off by default):** emits ALL channel URLs as proxy
URLs; the proxy answers NATIVE / XUMO_SSAI channels with a 302 to the upstream
URL including query (`ads.*` intact for Xumo), fully rewrites `AMAGI_SSAI`
channels, and mints then 302s `SESSION` channels to `stream_manifest`.
Justifications:
(a) observability — every playback start touches the proxy, enabling per-channel
now-playing/last-failure telemetry in the UI; (b) drift insulation — upstream
URL-format changes become proxy-internal fixes rather than M3U regeneration
events. Documented tradeoff: in this mode the proxy is availability-critical
for ALL channels (an outage breaks playback start even for healthy native
streams). Default remains selective proxying (`AMAGI_SSAI` + `SESSION` +
`DISTRO_RESOLVE` + `STIRR_RESOLVE`).

## What FASTGen serves

- `GET /{provider}.m3u`  — M3U8 playlist for that provider
- `GET /{provider}.xml`  — XMLTV guide for that provider
- `GET /playlist.m3u` and `GET /epg.xml` — merged output across enabled providers
- `GET /logos/{provider}/{channel_id}.png` — locally cached channel logos
  (populated when `cache_logos` is enabled; otherwise unused)
- `GET /healthz` — JSON: per-provider status, last successful refresh, channel
  count, programme count, staleness flag (rich payload; stub `{"ok":true}` today)
- `GET /metrics` — Prometheus format (refresh duration, failures, channel counts)
- `GET /api/client-access` — last 30 days of non-UI pulls of root emit files
  (`/playlist.m3u`, `/epg.xml`, `/{provider}.m3u|xml`): per-file hit counts +
  last IP/time, plus a filterable event list (`file`, `ip`, `status`, `limit`).
  Guide UI uses `/api/guide/…` and is not counted. Status shows the summary;
  Access (`/access`) is the pull history table.

## Providers (initial set; architecture must make adding more trivial)

Implement a `Provider` interface (Fetch() → normalized []Channel + []Programme)
with two fetch strategies:

### Direct API adapters

**LG Channels US** — `GET https://api.lgchannels.com/api/v1.0/schedulelist`
with headers `x-device-country: US`, `x-device-language: en`, and a desktop
Chrome user-agent. Wire body is often **base64(zlib(JSON))** under
`Content-Type: text/plain` (no `Content-Encoding`); `Accept: application/json`
is rejected (406). Older responses were plain JSON — fastgen accepts both and
stores decoded JSON in cache. Response JSON shape:
`categories[] → channels[] → programs[]`.
Channel fields: `channelId`, `channelName`, `channelNumber` (string!), 
`channelLogoUrl`, `mediaStaticUrl` (normalize query: strip junk; keep `ads.*`
Xumo/CloudFront SSAI keys and neutralize `[IFA]`/`[LMT]`-style client macros),
`providerId`,
`channelGenreName`. Programs: `programTitle`, `description`, `startDateTime`/
`endDateTime` (ISO8601 Z → XMLTV `20060102150405 +0000`). Dedupe channels by
`channelId` across categories. Some LG channels are Xumo CloudFront SSAI
(`XUMO_SSAI` once `ads.*` is retained); stripping those query keys breaks
origin interpolation. Guide depth is short (~12–14h); there are no
time-range query params to request more.

### i.mjh.nz adapters (matthuisman's aggregated feeds)

Base: `https://raw.githubusercontent.com/matthuisman/i.mjh.nz/master/{Provider}/`

- channels: `.channels.json.gz` → `{slug, regions: {us: {channels: {id: {name,
  chno, logo, group, description}}}}, headers}`
- EPG: `{region}.xml.gz` — already valid XMLTV; filter to emitted channels only
- Stream URL: `https://jmp2.uk/{slug-with-id-substituted}` where slug comes from
  the metadata `slug` field (e.g. Samsung = `stvp-{id}`)

**GOTCHA:** PlutoTV's and Plex's `.channels.json.gz` have NO `slug` field. Slug
must be configurable per provider (`plu-{id}.m3u8` for Pluto, `plex-{id}.m3u8`
for Plex). Never emit a bare `{id}.m3u8` fallback silently — log a warning if
slug is missing and no override exists.

Shipped mjh providers: PlutoTV (us), SamsungTVPlus (us), Roku, Plex (us).

**GOTCHA (Roku):** Roku's `.channels.json.gz` has NO `regions` wrapper --
channels live at the top level -- and no `slug` field (use `rok-{id}.m3u8`,
WITH extension). Its `group` is a LIST (`groups`); take the first element.
Its EPG file is `all.xml.gz`, not `{region}.xml.gz`. The mjh adapter must
handle both shapes (regioned + regionless).

**GOTCHA (Plex):** Plex is *tagged-region* shaped — channels live at the top
level with a per-channel `regions: ["us", …]` array. The `regions.us` block
has headers/name only (no nested `channels` map). Filter top-level channels by
tag membership for the configured region; EPG is still `{region}.xml.gz`.

### System-wide `regions` (provider input)

Top-level config `regions` (YAML string or sequence; env `FASTGEN_REGIONS`) is the
**only** geography knob. Default `US`. Region codes are normalized internally to
**ISO 3166-1 alpha-2 uppercase** (`us`/`Us` → `US`; Distro fantasy `qq` → `QQ`)
so filters never show mixed case. It controls what region-capable adapters
**scrape and carry** — not emit/filter rules (exclusions, health, cross-provider
dedupe, etc.).

| Adapter | Consumes `regions`? |
|---------|---------------------|
| Pluto, Samsung, Plex (MJH) | Yes — merge each code present in upstream metadata; skip unknowns |
| DistroTV | Yes — merge feeds per geo; add `QQ` explicitly if needed |
| Roku, LG, Tubi, Xumo, TCL, LocalNow | No — ignore |

**#47 spike (2026-08-05):** remaining candidates were probed for real multi-geo
feeds. **None** are viable today — keep them ignoring `regions` until upstream
changes. Summary: LG `x-device-country` only works for `US` (non-US → API 500);
Tubi/Xumo/TCL/LocalNow are single US published-pair artifacts (no `*_ca` files);
Roku mjh is still regionless (`channels`+`headers` only, guide `all.xml.gz`).
Full notes on GitHub [#47](https://github.com/j27-aurum/gofast/issues/47).

When more than one **usable** MJH region is configured, catalog channel ids become
`{REGION}_{upstreamId}` (uppercase region) so lineups do not collide; stream
slugs still use the upstream id. A single usable region keeps bare upstream ids
(stable vs prior releases).

**Regional twins:** at refresh, same provider + same upstream id + different
region collapses to one channel (preferred = earliest entry in `regions`).
Cross-provider title Dedupes still apply afterward. Configurable / Dedupes-UI
control for this is a follow-up.

`providers.*.region` is ignored (warned at load).

### Published-pair adapters (maintained upstream M3U + EPG consumed directly)

Third fetch strategy: some providers are best consumed via a community-
maintained, pre-generated M3U + XMLTV pair, re-emitted with our filtering,
labeling, numbering, and validation layered on top:

- **Xumo Play** (~400 ch): M3U + EPG from
  `https://raw.githubusercontent.com/BuddyChewChew/xumo-playlist-generator/main/playlists/xumo_playlist.m3u`
  and `.../xumo_epg.xml.gz`. Streams are often CloudFront SSAI URLs (`XUMO_SSAI`
  when `ads.*` query keys are present). No
  upstream tvg-chno (synthesize).
- **DistroTV** (small live lineup): Distro’s jsrdn feed
  `https://tv.jsrdn.com/tv_v5/getfeed.php?type=live` (+ optional `&geo=`) and
  EPG `https://tv.jsrdn.com/epg/query.php`. Catalog `StreamURL`s are opaque
  (`distro://channel/{geo}_{id}`); dialect **`DISTRO_RESOLVE`**. FASTProxy
  refreshes the feed at tune-in, substitutes ad macros, then 302s or rewrites
  (Amagi / Origin-locked hosts). The old vraomoturi published DAI M3U is
  abandoned (event IDs 404). Default **disabled**; enable with gen +
  `--profile proxy` and `proxy_base_url`. No tvg-chno (synthesize from 6000).
- **STIRR** (~155 ch): stirr.com API list
  (`/api/videos/list/?…&content_type=4&no_limit=true`) + bulk `/api/epg`
  (plus soft-fetched per-row Wurl `epg_url` when missing from bulk). Catalog
  `StreamURL`s are opaque (`stirr://channel/{videoid}`); dialect
  **`STIRR_RESOLVE`**. FASTProxy POSTs `/api/v2/videos/{id}/playable`, fills
  `[vx_nonce]`, returns the **master** HLS URL (mostly Aniview SSAI), then
  302s or rewrites. Refresh soft-audits Aniview masters: HTTP 422 `error=CON`
  (deleted SSAI config) gets hard filter **`dead SSAI (upstream config gone)`**
  and is dropped from M3U/XMLTV (still visible in Channels UI). Tune-in
  `stirr_resolve_fail` also feeds playback health. Default **disabled**; needs
  gen + `--profile proxy` and `proxy_base_url`. Synthesize from 11000 when
  upstream `channel_number` is missing.
- **LocalNow** (local news/weather by market -- the only local content in the
  set): M3U `https://www.apsattv.com/localnow.m3u` + EPG
  `https://raw.githubusercontent.com/BuddyChewChew/localnow-playlist-generator/refs/heads/main/epg.xml`.
  CAUTION: playlist and EPG are maintained by different people; validate id
  match rate at refresh and surface it in health. apsattv may 403 datacenter
  IPs and non-browser UAs.
- **Tubi** (~170+ ch US): M3U + EPG from BuddyChewChew
  [app-m3u-generator](https://github.com/BuddyChewChew/app-m3u-generator)
  `playlists/tubi_all.m3u` and `playlists/tubi_epg.xml` (plain XML, daily
  Actions). Community scrape upstream — same published-pair risk class as Xumo.
  No tvg-chno (synthesize). Stream URLs may carry short-lived JWTs refreshed by
  the upstream generator.
- **TCL** (~400+ ch): M3U + EPG from BuddyChewChew
  [tcl-playlist-generator](https://github.com/BuddyChewChew/tcl-playlist-generator)
  `tcl.m3u8` + `tcl_epg.xml` (plain XML; file extension is `.m3u8` but content is
  EXTINF M3U). Same published-pair risk class as Xumo/Tubi. IdeoNow gateway
  (`gateway-prod.ideonow.com`) requires auth — do not put an authenticated API
  client in-tree. Sample streams include Amagi playouts (`AMAGI_SSAI` → proxy)
  and Wurl masters with `ads.*` (`XUMO_SSAI` / `NATIVE`). No tvg-chno (synthesize).

Published-pair requirements: parse EXTINF attributes + display name + stream
URL; dedupe by normalized tvg-id; sanitize the upstream EPG for bare
ampersands BEFORE parsing (upstream generators have shipped invalid XML);
rebuild the EPG through the XML marshaler so output validity is guaranteed
regardless of input.

**Direct-API notes:** Plex ships via mjh (`i.mjh.nz/Plex/`); a direct anon-token
API (`clients.plex.tv` → `epg.provider.plex.tv`) is not needed unless mjh
regresses. In-tree Tubi HTML/`/oz/epg/programming` and IdeoNow TCL gateway
clients are deliberately not pursued — use the published-pairs above.

**Deprioritized with reasons:** Amazon Fire TV Channels (Freevee successor;
auth/DRM-heavy, no stable community source -- if attempted, build the adapter
and let the classifier measure the DRM fraction before investing), Sling
Freestream and Fubo free tier (DRM-heavy), Vizio WatchFree+ (device-bound
streams).

## Normalization & output rules

- **Channel id normalization (critical):** the tvg-id exists to JOIN the M3U
  to the XMLTV (`tvg-id` == `<channel id>` == `<programme channel>`); players
  also use it as the channel's persistent identity across refreshes. Since we
  emit both files, normalize ids with one deterministic function applied to
  ALL THREE locations: collapse whitespace to `_`, strip control characters
  and quotes, restrict to `[A-Za-z0-9._-]`. Real case: DistroTV ships ids with
  embedded spaces (`dtv_EPGACE TV`), which breaks guide matching in Jellyfin.
  Normalization must be stable run-to-run (same input -> same id, always) or
  players treat every refresh as a new channel lineup.
- **Channel numbers:** per-provider integer `channel_number_offset` added to the native
  number when the upstream provides one. Emit as `tvg-chno` in M3U and
  `<lcn>` in XMLTV. `<lcn>` is a deliberate compatibility extension rather
  than part of the XMLTV DTD; Jellyfin gets tuner numbering from M3U
  `tvg-chno`. Sort each playlist globally by positive final number, then put
  unnumbered channels last with provider/id tie-breakers. For providers with no
  upstream numbering (Xumo, Tubi, DistroTV, LocalNow):
  `synthesize_channel_numbers: <base>` assigns sequential numbers through a
  persisted per-provider id->number map. First-seen assignments are reused
  forever (including after removal/reappearance), stored atomically with the
  provider generation metadata, and never derived from mutable playlist order.
- **Provider labels:** per-provider `label` (LG/Pluto/Samsung). Display name in
  M3U = `{name} · {label}`. `group-title` = `{label}: {group}`. Keep `tvg-name`
  as the clean unlabeled name.
- **Group taxonomy (cross-provider merge + disable, `groups` in `config.yaml`):**
  optional, off by default (legacy `{label}: {group}` folders). When enabled the
  provider prefix is dropped and `group-title` becomes the resolved group name:
  - **Auto-merge:** identical upstream group strings across providers collapse to
    one folder on their own (no config needed).
  - **Merge:** an operator `merges` entry folds differently-named upstream groups
    (`NEWS`, `News & Info`, `Noticias`) into one canonical `name`. Members match
    trim + case-insensitively across all providers; unlisted strings emit bare.
  - **Disable:** a merged group (`enabled: false`) or a `disabled` selector — bare
    `groupName` (all providers) or `providerID/groupName` (one provider) — drops
    its channels from **both** M3U and XMLTV. Disable wins over emit. Disabled
    channels stay in the API/UI with `filter_reason = disabled group "X"`; upstream
    `Channel.group` is never mutated (detail still shows the source group).
  - **Precedence vs per-channel emit:** a `providers.<id>.channel_emit.<normalized_id>.group`
    customization wins over taxonomy for that channel's emitted `group-title`.
  - Managed in the UI Groups editor; saving writes the `groups` block back to
    `config.yaml` (comment-preserving) and applies **live** (re-emit, no restart).
- **Programme category taxonomy (merges only, `categories` in `config.yaml`):**
  optional, off by default. Upstream XMLTV `<category>` labels round-trip on
  `Programme.categories`; when taxonomy is enabled, merges canonicalize labels
  into `emitted_categories` for emit and Guide coloring (first label picks the
  block color). There is **no disable** — a category is a label on an airing,
  not a channel you can drop. Managed in the UI Categories editor; `GET/PUT
  /api/categories` persists and hot-reloads like Groups.
  **Jellyfin note:** Live TV listings providers expose exactly four designation
  synonym lists (Movies / Kids / News / Sports), matched case-insensitively
  against emitted `<category>` strings; other labels remain generic genres.
  Align Categories merge names with those lists, or extend Jellyfin’s lists to
  include whatever fastgen emits — no GoFAST Jellyfin-specific category remap.
- **Per-channel emit customization (`channel_emit`):** under each
  provider, a map keyed by normalized channel id customizes **what fastgen
  emits** (not upstream fetch/raw): display name, group-title, channel number,
  logo URL, and export include/exclude (`auto` / `enabled` / `disabled`).
  Force-include may clear soft operator blocks (regex exclusions, disabled
  group, `exclude_unhealthy`) but never DRM, missing identity/stream, or
  needs-FASTProxy. Rows for temporarily absent channels are retained until
  reset. Edited on the channel detail “Provider vs Fastgen” table (per-field
  Customize checkbox); persisted via `config.Store` and applied live on re-emit.
- **Dedupes (cross-provider same-title clusters):** many FAST providers
  package the same brand/channel under different stream URLs (not URL-identical).
  FASTGen clusters channels whose display titles match after stripping
  ` · {provider label}` and folding case/`&`/`+`/punctuation — **without**
  stripping brand tokens, so `BET`, `BET Pluto TV`, and `BET Her` stay distinct.
  Clusters require ≥2 providers. The Dedupes UI (`/dedupes`, `GET /api/dedupes`,
  `PUT /api/dedupes/apply`) lets operators   Keep selected / Keep preferred /
  Keep all / Drop all; apply writes `channel_emit.export` (and `channel_emit.dedupe:
  true` on losers) plus optional
  `dedupe.preferred_providers` / `dedupe.keep_all_keys`). No silent auto-purge;
  LLM / health auto-pick are follow-ons. Dedupes losers get FilterReason
  `duplicate` (distinct from manual `emit disabled`).
- **Exclusion reasons (multi):** a channel may carry **multiple**
  `FilterReason` values at once (`filter_reasons` + primary `filter_reason`).
  Soft reasons (regex exclusion, disabled group, unhealthy, emit disabled,
  duplicate) clear under force-include / `export:enabled`; hard reasons (DRM,
  unsupported classification, needs-FASTProxy when proxy is unset, missing
  identity/stream, **absent from provider**) never soft-clear. `EmittedURL` is
  minted whenever a playback path exists (including on duplicate/regex-excluded
  channels); playlist membership is gated only by exclusion reasons via
  `ForExport`. UI Status filter meanings are listed under **Web UI** below.
- **Exclusion filters:** per-provider list of case-insensitive regexes matched
  against stream URL + provider id + channel name (fast first pass; the
  classifier probe is the authoritative gate). Filtered channels are removed
  from BOTH the M3U and the XMLTV.
- **Classifier probe details:** URL heuristics first (no fetch): Google DAI
  (`dai.google.com` + `/linear/hls/`) → `SESSION`; any `ads.*` query key →
  `XUMO_SSAI`. Else fetch master playlist -> first variant -> inspect first ~5
  segment lines. `AMAGI_SSAI` if segments contain `/beacon/` or lack a media
  extension (.ts/.aac/.mp4/.m4s) before the query string. Fetch errors classify
  as NATIVE (never drop a channel on a transient network failure). Run probes
  concurrently (bounded worker pool). Amagi context: most FAST channels are
  played out by Amagi (`*.playouts.now.amagi.tv`); the beacon dialect is their
  SSAI ad-tracking format and appears across unrelated "providers" (BBC, A&E)
  because they share the same playout vendor. Legacy channel-attr / meta value
  `BEACON` canonicalizes to `AMAGI_SSAI`.
- **DRM:** i.mjh.nz channel records with a `license_url` field are Widevine
  encrypted; drop them unconditionally (65 of Samsung's US channels at time of
  writing).
- **XML generation:** MUST use `encoding/xml` marshaling. Never build XML via
  string concatenation or templates — an upstream generator we replaced shipped
  unescaped `&` in `<display-name>` and it killed Jellyfin's entire guide parse.
  Preserve legitimate punctuation, including quotes and apostrophes.
  Structurally invalid output must be unrepresentable. Validate by re-parsing
  the serialized bytes and checking unique channel ids/programme references
  before publishing to the cache. This validates GoFAST's XMLTV profile and
  well-formedness, not DTD conformance because of the `<lcn>` extension.
- **M3U values:** strip/replace double quotes in quoted attributes and collapse
  controls/newlines in presentation text so one channel cannot inject another
  record. Playback URLs are never rewritten: reject any URL containing a
  control/newline and retain last-known-good.

## Logo caching

Controlled by `cache_logos` / `FASTGEN_CACHE_LOGOS` (default **false**).
Config UI label: “Cache + rewrite logos” — one knob; there is no separate
rewrite toggle.

- **Off:** leave upstream CDN `tvg-logo` / XMLTV `<icon>` / API `logo_url`
  unchanged (browsers and Jellyfin fetch artwork themselves).
- **On:** download every channel logo at refresh time into an on-disk cache
  (`/data/cache/{provider}/logos/{id}.{ext}`), and rewrite those URLs to this
  service's own `/logos/...` paths under `base_url` / `FASTGEN_BASE_URL`
  (required when enabled — e.g. `http://fastgen.lan:8180`). Rationale: Jellyfin
  fetches artwork itself and chokes on hostile CDNs.

When caching is enabled:

- Fetch logos with per-provider request headers (mjh metadata's `headers` field,
  e.g. Samsung wants `user-agent: okhttp/4.12.0`).
- Per-channel emit `logo_url` overrides are cached and rewritten the same way
  as provider logos (warm runs after emit save / `base_url` change, not only
  on provider refresh).
- **Per-host TLS policy:** `tvpnlogopus.samsungcloud.tv` serves a chain rooted
  in DigiCert Global Root CA (G1), which Mozilla/Chrome distrusted 2026-04-15
  and which is absent from current system stores. Support per-host extra root
  CAs (PEM in config) and, as a last resort, per-host `insecure_skip_verify`
  for artwork-only hosts. Default everything else to system roots. Never apply
  relaxed TLS to stream or EPG endpoints.
- On hard upstream failures (HTTP 403/404), clear `logo_url` and set
  `logo_error` on the channel. Soft failures (5xx/network) keep the upstream
  CDN URL in the export — intentional so a transient artwork outage does not
  blank logos.
- Cache revalidation on each provider refresh: if the upstream logo URL changed,
  fetch unconditionally; if it is unchanged and the CDN sent ETag/Last-Modified,
  use a conditional GET; if there are no validators, skip re-download while the
  on-disk file is within max-age (default 7d).
- First publish after refresh may briefly still show CDN URLs until the
  background warm finishes — check `GET /api/status` (`logos.running`) before
  verifying playlists.
- Strip per-programme `<icon>` elements from mjh EPGs (thousands of them, they
  flood Jellyfin's pre-cache with fetch failures and barely render).

## Refresh & cache semantics (last-known-good, always)

- Per-provider refresh interval (default 6h; LG default **3h**) with ±10%
  jitter; independent goroutines; one provider failing never blocks others.
  Every 5 minutes each provider logs `next_refresh_at` / `refresh_in` (or
  `refresh_state=in_progress` while a refresh is running).
- **Refresh vs EPG horizon:** every refresh pulls the **full** upstream guide
  (no artificial time-window trim). The effective `refresh_interval` is clamped
  to ≤ half of the known ahead-horizon (floor 1m) so the guide cannot expire
  before the next fetch. Horizon is empirical (`guide_end − fetched_at`) after
  a successful publish, else the provider's declared `ExpectedGuideHorizon`
  (code default, not YAML):

  | Provider | Expected guide horizon | Default refresh |
  |----------|------------------------|-----------------|
  | LG | ~12–14h (upstream limit) | 3h |
  | DistroTV | ~24h (jsrdn) | 6h |
  | Pluto / Samsung / Roku / Xumo / Tubi / LocalNow | ~48h (Tubi measured ~46h) | 6h |

  When clamping changes the schedule, slog warns and Provider Detail /
  `/healthz` / `/metrics` expose configured vs effective intervals and
  `guide_hours_ahead`. Exhausted horizon (`guide_end` before now, or ahead
  shorter than the effective interval) logs `guide_horizon_exhausted` but does
  not fail HEALTHCHECK.
- A refresh is published only if it passes gates: **upstream catalog** size ≥
  per-provider `min_channels` (count before Dedupes / emit-disable / dialect
  filters — not the exported M3U size), XML re-parses, programme count > 0.
  Otherwise keep serving the previous good snapshot. `/healthz` marks a
  provider `stale: true` when `status.json` still holds a `LastError` while
  last-known-good is served.
- Snapshots are atomic swaps of in-memory objects (RWMutex or atomic.Pointer);
  additionally persist last-good output to `/data/cache/` so a container
  restart serves immediately even if upstreams are down at boot. Per-provider
  and aggregate artifacts use immutable generations selected by an atomic
  `current` pointer so M3U+EPG publish as a pair. Operators can inventory and
  soft-purge via `GET /api/cache`, `POST /api/providers/{id}/cache/purge`,
  `POST /api/cache/purge`, and `DELETE /api/logos…` (UI: **Cache** page). Soft
  purge deletes non-current generations (and staging leftovers) while keeping
  the serving `current` until refresh commits; optional `?logos=1` clears logos.
  Orphan sweep removes unconfigured provider dirs, out-of-lineup logos, and
  generations beyond current+1. Channelattr is inventory-only (never purged).
- **Provider catalog deltas (presence):** each successful refresh (and warm
  restore/reapply) records channel **add/drop** in `channelattr` as
  `presence` events (`present` / `absent`), keyed like health and
  classification. Presence means membership in the provider catalog after
  `prepare` — not M3U export (Dedupes / emit-disable / DRM exclusions do not
  count as drops). Diffs use Current presence, not the live Feed, so reboot
  does not re-treat every channel as new. Classification change history
  continues alongside. Cross-channel `EventsSince` assembles report windows
  (adds/drops/class changes); see ARCHITECTURE for the full narrative.
  Event history older than 90 days is pruned; current rows are kept.
  **UI:** `GET /api/channels` appends ghost rows for Current `absent` (Status
  filter **Absent**); Channel Detail shows presence history; Status shows
  **Absent now** and **Dropped (7d)** via `GET /api/presence/summary`.
- **Daily ops-report email (`ops_report`):** optional once-per-local-day HTML
  digest (multipart plain-text + rich HTML matching the operator UI brand).
  Schedule is IANA timezone + `HH:MM` (default `America/Los_Angeles` / `00:00`)
  with a 2h grace catch-up; always-send even when deltas are empty. Content:
  per-provider refresh status + durable refresh tallies since last official
  send, fleet health rollup, presence adds/drops, classification changes, and
  health **status transitions** (not every probe tick). SMTP is SES-friendly
  (`host`/`port`/`starttls`/`username`/`password`); password may live in YAML
  for local/dev but **prefer `FASTGEN_SMTP_PASSWORD`** (env wins; Config UI
  locks the field). State + archives live under `{data_dir}/ops_reports/`
  (`state.json`, `report-*.json`, 90-day prune) — not under `channelattr/`.
  Manual actions: **Test SMTP** (smoke, not archived), **Generate and Send
  Report** (full preview: archive, no `last_success` / no tally reset),
  **Resend** from archive (replay stored MIME). Official daily fire updates
  `last_success_*` and resets tallies. APIs under `/api/ops-report/*`; Settings
  UI section + Status snippet when enabled.
- Structured refresh success/failure logs include counts and `duration`, plus
  `guide_horizon` / `refresh_interval` / `effective_interval` on publish.
- Cache-backed playlist/guide responses carry a strong body `ETag` (SHA-256);
  honor `If-None-Match` with 304s (Jellyfin refetches often).

## Config

Follow [12factor.net/config](https://12factor.net/config): deploy-varying values live
in the environment; the codebase must not embed credentials or private hostnames.
Both `fastgen` and `fastproxy` listen via the shared **`PORT`** env var
(`8180` or `:8180`). `base_url` / `FASTGEN_BASE_URL` is the public origin
clients use (logos, absolute links): include the port when not on 80/443
(e.g. `http://fastgen.lan:8180`); omit it behind HTTPS reverse proxy
(`https://gofast.example.com`); no trailing slash. Optional YAML on the data
volume (`/data/config.yaml`) may hold structured, non-secret settings
(including per-provider blocks). Gen-only overrides: `FASTGEN_BASE_URL`,
`FASTGEN_DATA_DIR`, `FASTGEN_PROXY_BASE_URL`, `FASTGEN_PROXY_ALL`. Env always
wins over the file. Precedence: code defaults →
YAML (if present) → env. Include a documented example (`config.example.yaml`)
in the repo — never a filled production config.

`config.yaml` is **operator-writable**: mount `/data` (or the config path) read-write.
On first boot with no file present, fastgen generates `/data/config.yaml` from the
baked-in code defaults (deploy-varying env values are not baked in). App-managed
settings persist back through a shared, atomic, comment/unknown-key-preserving YAML
writer that keeps a `.bak` of the prior bytes. A read-only mount surfaces a clear
"config is read-only" error instead of failing silently.

**Settings UI + live hot-reload.** The UI Settings page edits config as
typed controls (never a raw YAML editor). The rule is strict: **in the UI = live,
in the file = restart.**

- A UI save goes through one path (`config.Store.Save`): apply dotted-path ops to
  a candidate copy of `config.yaml`, validate the candidate through the exact boot
  load path (`config.New`, env overlay included), atomically replace the file
  (keeping `.bak`), reload the in-memory snapshot, then kick every registered
  subsystem `Reloader` (logging → health → refresh) to reconcile itself. Saves
  carry a revision (SHA-256 of the file bytes) and stale saves are rejected.
- Everything the UI exposes applies live: base/proxy URLs, `proxy_all`,
  `cache_logos`, HTTP timeout, log level, all health knobs, the `groups` /
  `categories`
  taxonomy, and every per-provider setting including **enable/disable**.
  Disabling a provider stops its refresh goroutine, hides its channels, and 404s
  `/{id}.m3u` + `/{id}.xml`; its cache generations and channel attributes stay on
  disk so re-enabling restores instantly (warm) or fetches (cold, first enable).
- **Restart-only settings get no UI control**: `listen`/`PORT` and `data_dir`
  are shown read-only in a Deployment panel ("edit config.yaml and restart").
  Hand-edits to the file are never watched; they take effect on the next boot.
- Env-shadowed fields render locked with a "set by `<VAR>`" badge — env always
  wins, so writing them to the file would silently do nothing.
- Secrets stay env-only (none exist in today's config surface); the API/UI design
  supports masked/redacted fields so a future secret is never persisted to
  `config.yaml`.

## Operational requirements

- Structured logging (slog), one line per refresh with counts and duration.
- Graceful shutdown; refresh contexts cancelled on SIGTERM.
- Timeouts on every outbound call (default 60s); retry with backoff (3 tries).
- No panics on malformed upstream data — skip the bad record, count it, log it.

## Docker

- **CI builds artifacts; production images only package them.** Pipeline: Node
  builds the React UI → Go embeds it and compiles static `fastgen` /
  `fastproxy` → tests → `Dockerfile.prod` copies those binaries into
  `gcr.io/distroless/static` (plus ca-certificates; healthcheck helper as
  needed). Do **not** re-run `npm` / `go build` inside the production image
  build. Images MUST include an up-to-date ca-certificates bundle; TLS trust
  is a first-class concern.
- Produce **two images**: `fastgen` and `fastproxy` (targets or twin final
  stages in `Dockerfile.prod`).
- Optional `Dockerfile` may multi-stage build from source for local compose
  convenience; it is not the GHCR ship path.
- Volume on gen: `/data` (config, last-good snapshots under `cache/`, durable
  logos under `cache/{provider}/logos/` when enabled).
- Expose gen `8180` (and document proxy port, e.g. `8181`). Healthcheck hits
  `/healthz` on each service.
- Provide `docker-compose.yml` for local/dev and `docker-compose.prod.yml` for
  Portainer/homelab (pull GHCR; `fastgen`/`gen` required; `fastproxy`/`proxy`
  optional via compose profile). Document Jellyfin tuner/guide URLs and
  `proxy_base_url` when the proxy profile is enabled.

## Testing

- Unit tests for: normalization (offsets, labels, quote stripping), exclusion
  filters (must drop a channel whose stream URL contains `dinospluto-lgus`),
  XMLTV time conversion, the Pluto missing-slug default, and XML validity with
  hostile inputs (`A&E Crime 360`, names containing `<`, `"`).
- Golden-file tests for M3U and XMLTV output from fixture JSON.
- An integration test with `httptest` fake upstreams covering: failed refresh
  keeps last-known-good; min_channels gate refuses a gutted lineup; 304 flow.

## Web UI (required)

Embedded web UI **in the fastgen binary**: a React (Vite) app under `web/` is
built to static assets and embedded with Go `embed`, then served by the same
fastgen process — no separate frontend container. Node is required only to
*build* the UI (CI/Docker/`npm run build`); runtime stays one Go binary.
Proxy has no product UI. Ship the UI foundation early and feather features as
gen capabilities land (classification, export reasons, health, config editor).

Core view — the channel table: every channel from every provider (including
**Absent** ghosts no longer in a provider catalog) with a classification badge
and one or more status badges. The grid stays lean (name + logo thumb; no
raw/normalized ids or logo errors in-row). Filterable/searchable by provider,
group, class, **status**, health, and name.

### Channels Status filter (what each value means)

Status is **playlist / catalog outcome**, not stream dialect (that is Class)
and not probe health (that is Health). A channel may match **multiple** status
kinds (ANY-match filter). Wire reasons live in `filter_reasons` /
`filter_reason`; UI kinds map from those (plus in-lineup / proxied when not
excluded).

| Status | Meaning |
|--------|---------|
| **In lineup** | Included in the emitted M3U/XMLTV (Jellyfin can tune it). Direct upstream URL (not rewritten through FASTProxy). |
| **Via proxy** | Included in the playlist, but `emitted_url` goes through FASTProxy (Amagi rewrite, SESSION mint, or `proxy_all`). |
| **Duplicate** | Dropped by Dedupes (`channel_emit.dedupe` / FilterReason `duplicate`). Still in the provider catalog; not in M3U. Distinct from manual emit-disable. |
| **Absent** | No longer in the provider’s **catalog** after refresh (upstream dropped/renamed the id). Ghost row from channelattr presence; not on the live feed; not exportable. Open detail for presence history. |
| **Needs proxy** | Would need FASTProxy to play (typically Amagi SSAI / SESSION / Distro) but `proxy_base_url` is unset, so it is blocked from export. |
| **DRM blocked** | Widevine / license-gated; hard-dropped from export. |
| **Unsupported class** | Classification the emitter does not know how to handle; hard-dropped. |
| **Disabled group** | Group taxonomy disabled this channel’s group; soft-dropped from export. |
| **Unhealthy** | Health is down/degraded and `exclude_unhealthy` is on; soft-dropped. |
| **Emit disabled** | Operator set `channel_emit.export: disabled` (manual), without the Dedupes `dedupe` marker. |
| **Regex exclusion** | Matched a provider exclusion regex (URL / id / name). |
| **Missing id** | No usable normalized identity for emit. |
| **Missing stream** | No stream URL to emit. |
| **Excluded (other)** | Has an exclusion reason that does not map to a more specific kind above. |

**Not the same as Health:** Healthy / Degraded / Down / Untested answer “does it play right now?” and use a separate filter. A channel can be **In lineup** and **Down**.

**Not the same as Class:** NATIVE / Amagi SSAI / SESSION / Distro resolve / STIRR resolve / Xumo SSAI / DRM answer “what dialect is this?” Class DRM often coincides with Status **DRM blocked**, but Class is the dialect badge; Status is the export/catalog outcome.

Per-channel detail (`/channels/{provider}/{normalizedId}`): export status and
all filter reasons, DRM `license_url` evidence, upstream vs emitted URL, raw
and normalized ids, logo URL or `logo_error`, **catalog presence history**,
health/probes, a compact **Guide** strip (Now / Next + expandable programme
list from in-memory EPG), and **per-field Customize** controls on the Fastgen
export column (name, number, group, logo, in-export) when the channel is still
in the catalog. Uncheck uses the fastgen-produced default; save applies live.

Provider list shows triage summaries (exported / excluded / stale / last
success). Provider detail holds full rollups (classifications, filter
reasons, guide coverage) mirroring the eventual rich `/healthz` shape.

Config editing for operator settings (exclusion regexes, channel number offsets,
labels, proxy settings, per-channel emit customizations), persisted to the YAML
config through `config.Store`.

Acceptance test for the UI: a user wondering why a specific LG channel is
missing from Jellyfin can locate it in the table (provider / search), open
the channel detail page, and read export status plus a one-line reason in
under ten seconds.

## FASTProxy requirements

- Separate `cmd/fastproxy` binary and Docker image (not a gen mode flag).
  Image is `debian:bookworm-slim` + **ffmpeg** (Class B encode; not distroless).
- **Class A / dialect endpoints:** `/stream/{provider}/{id}.m3u8`, rewritten
  variant paths `/s/{sid}/{n}`, `/seg/{token}` (Amagi shuttle), 302 for NATIVE /
  XUMO under `proxy_all`, SESSION mint → 302, Distro/STIRR resolve → 302 or rewrite.
- **Class B endpoint:** `/stable/{provider}/{id}.ts` — demux-stable MPEG-TS pipe
  (`video/mp2t`). Gen emits this URL when Class B is set and `proxy_base_url` is
  configured (Pluto fixture in #56; general detection in #57). Class B wins over
  `/stream/` when both A and B apply; proxy may loopback to `/stream/` for Amagi
  ingest then encode. Knobs: `FASTPROXY_DEMUX_STABLE_MAX` (default 2),
  `FASTPROXY_DEMUX_STABLE_SIZE` (default `1280x720`), `FASTPROXY_FFMPEG`.
- **Amagi (`AMAGI_SSAI`):** Class A only on `/stream/` — not a DVR guarantee.
  Relative URL resolution; rewrite variants/segments/`#EXT-X-KEY`; short-TTL
  sessions; never strip ads (beacon GET still fires).
- **SESSION:** parse DAI event id; `POST …/stream`; 302 to `stream_manifest`;
  short-TTL mint cache; telemetry `session_mint` / `session_mint_fail`.
- Never HEAD stream endpoints (SSAI often rejects HEAD).
- **Headless control-plane hop:** `FASTPROXY_GEN_URL` required. Origin pull +
  async `POST /api/proxy/events` (includes `demux_stable_*` snapshot fields and
  active encode sessions). Gen glass: Status + Proxy tab. Media path never
  blocks on ingest.
- Tests: Amagi rewrite fixtures; SESSION mint httptest; Class B slot exhaustion +
  stub-ffmpeg pipe; Pluto emission → `/stable/…ts`.
- Glossary: [`docs/TERMINOLOGY.md`](TERMINOLOGY.md) (Class A / Class B).

## Additional test cases (from production failures)

- HEAD vs GET: health checks must use GET (prefer Range; retry without Range on
  416); assert no HEAD requests are issued to stream endpoints.
- Classification migration: a channel flipping NATIVE→AMAGI_SSAI between refreshes
  must not change its emitted URL when proxy_all is on. Legacy `BEACON` reads as
  `AMAGI_SSAI`.

## Stream health validation (required; distinct from classification)

Classification (NATIVE / AMAGI_SSAI / SESSION / DISTRO_RESOLVE / STIRR_RESOLVE / XUMO_SSAI / DRM) answers "what
kind of stream is this" and decides export. Health answers "does it actually
play right now" and is a per-channel time-series. Keep them separate: health
annotates by default and gates export only by explicit opt-in.

**Mental model:** classifier and health are independent labels: a channel can be
`NATIVE` and down at the same time. Classifier inspection is not a health probe
and does not drive the health FSM.

**Health probe levels:**
1. **Health L1 — segment** — fetch the first media segment via GET on ProbeURL (emitted/export
   URL when set): prefer Range; plain GET on HTTP 416; soft-retry timeout/5xx;
   accept AES-encrypted segments. Persist HTTP status, duration, final URL,
   bytes, range flags. Catches dead playouts, 403s, geo-blocks, empty segments.
2. **Health L2 — decode** — run ffprobe against ProbeURL; PASS requires a **video** stream
   plus non-null format. Pass channel RequestHeaders. Scheduled Health L2 (when on)
   always probes degraded/down/untested and samples healthy (`l2_healthy_sample`);
   concurrent probes capped per host.

**Default posture — passive first, active minimal.** Design constraint: many
installs must not collectively generate bot-fingerprint probe traffic or fake
ad impressions against free services. Therefore:
- The classifier runs every refresh. It inspects stream dialect and fetches no
  media; it is not part of the health ladder.
- Health L1 (segment): daily baseline for `NATIVE` / `XUMO_SSAI`, and for
  proxy dialects (`AMAGI_SSAI`, `DISTRO_RESOLVE`, `STIRR_RESOLVE`) when an
  emitted proxy URL is set (probe through FASTProxy `/stream/…` — not opaque
  catalog URLs or upstream beacons). Never schedule Health L1 on `SESSION`
  (DAI mint = paid fake tune). Manual/Test-now may use EmittedURL when set.
- Health L1 **retry lane:** channels left `degraded` / `down` after a check get
  a per-channel `next_retry_at` with exponential backoff
  (`15m → 30m → 1h → 2h → 6h`), then park until the next baseline fleet sweep.
  Same eligibility gates as baseline (SESSION never; proxy dialects only with
  EmittedURL). Healthy channels are not re-probed early.
- Health L2 (ffprobe): OFF by default; opt-in config for users who accept the
  tradeoff. Bounded worker pool, per-probe timeout, jitter over a configurable
  window (default 60m), randomized order — never a clockwork fingerprint.
  Skip any `RequiresProxy` dialect when `EmittedURL` is empty.
- Migration: the former health L2 segment name is now Health L1; the former
  health L3 ffprobe name is now Health L2. Old YAML aliases are accepted for
  one release.
- **Passive health (primary signal):** FASTProxy records the outcome of every
  REAL playback session (upstream playlist fetch result, segment flow,
  failure class) and feeds the health state machine. Watched channels are
  continuously validated by actual use at zero extra requests; unwatched
  channels honestly remain UNTESTED.
- **On-demand probe:** a "Test now" action in the UI channel detail runs one
  level-3 probe for that single channel — indistinguishable from a viewer
  tuning in. This is the sanctioned way to deep-test Amagi SSAI channels.

**Health event log:** persist probe results and (once FASTProxy lands)
playback outcomes as an append-only event log with a versioned schema —
`(schema_version, channel_id, source, outcome, failure_class, coarse
timestamp)`. The state machine and UI probe history are derived views over
this log. Schema changes must be versioned and backward-readable across
upgrades.

**HealthSource seam:** define health input as an interface; probe results and
playback telemetry are its two implementations.

**Probe paths:** on-demand and opt-in probes test Amagi SSAI channels THROUGH the
proxy URL (end-to-end validation of the rewrite chain); NATIVE / SESSION /
XUMO_SSAI channels at their emitted URL.

**Health states:** UNTESTED / HEALTHY / DEGRADED (recent intermittent
failures) / DOWN (N consecutive failures across probes and/or real playback
attempts, default 3). DOWN channels remain
exported but are badged in the UI and counted in /healthz; `exclude_unhealthy:
true` (default false) prunes them from output for users who prefer a
self-cleaning lineup — document that this causes lineup churn in Jellyfin.

**ffprobe fidelity:** ship ffprobe in the **fastgen** container as the default
judge; `ffprobe_path` is configurable so co-located deployments can point at
Jellyfin's exact jellyfin-ffmpeg binary (demuxer strictness has varied across
ffmpeg releases). Probes use GET semantics only — never HEAD (SSAI endpoints
reject HEAD).

**UI/metrics integration:** channel table gains a health column (state, last
probe time, failure streak, 30-day success rate); per-channel probe history in
the detail view; Prometheus metrics at provider granularity (healthy/degraded/
down counts) with per-channel detail via the JSON API to avoid label
cardinality explosion.

## Build order (follow this sequence; each milestone independently shippable)

1. **Shared core + classifier.** Channel model, config loading, provider
   adapters, and the stream classifier with its fixture tests (beacon-style
   Amagi playlist, clean playlist, relative-URL, fetch-error-keeps-channel)
   passing before anything else is built. The classifier is the load-bearing
   component; everything downstream trusts its buckets. UI shell may land in
   bootstrap so later milestones feather into a live surface.
2. **FASTGen.** Refresh loops, emission decision table, M3U/XMLTV output
   validated against golden files, logo cache, /healthz, ETag/304 flow,
   fastgen Dockerfile. MILESTONE: at this point the service fully replaces the
   existing Python batch script and is deployable on its own.
3. **Health subsystem.** HealthSource interface, tiered prober (level 1/2
   scheduling, level 3 on-demand), state machine
   (UNTESTED/HEALTHY/DEGRADED/DOWN), probe-history persistence. The passive
   playback-telemetry implementation lands with FASTProxy in milestone 5.
   Testable against httptest fixtures without real streams.
4. **Web UI polish.** Channel table with classification AND health columns,
   per-channel detail with filter reasons and probe history, provider rollups,
   config editing. Meets the ten-second acceptance test. (Foundation and
   feature slices should already be feathered from earlier milestones.)
5. **FASTProxy last.** Separate binary/image; largest and riskiest chunk
   (media path, session state, playlist rewriting). Building it last means
   milestones 1–4 deliver a complete, shippable gen product even if the proxy
   is deferred.

Do not interleave these phases; finish each milestone's tests before starting
the next.

## Deliverables

Complete repo: Go module `github.com/j27-aurum/gofast`, **two binaries**
(`cmd/fastgen`, `cmd/fastproxy`) and **two Docker images**, shared `internal/`
packages (providers, classifier, epg, m3u, logocache, proxy, server, config,
ui), tests passing via `go test ./...`, Dockerfile target(s),
`docker-compose.yml` (gen-only default + optional proxy profile), README with
Jellyfin setup instructions and the provider endpoint table. Keep dependencies
minimal: stdlib + yaml parser is the target; justify anything beyond that.
