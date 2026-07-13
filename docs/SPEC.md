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

**The classifier** probes each channel at refresh time and buckets it:
- `NATIVE`: plain media segments — plays anywhere
- `BEACON`: Amagi SSAI dialect — segment lines are extensionless tracking URLs
  (`/beacon/`, query-laden, no .ts) that ffmpeg's `allowed_segment_extensions`
  security check rejects
- `DRM`: has a Widevine `license_url` — never playable, no proxy can help

**Emission decision table** (in FASTGen):
- NATIVE → emit upstream URL directly (no proxy in media path)
- DRM → drop always
- BEACON → if `proxy_base_url` configured, emit
  `{proxy_base_url}/stream/{provider}/{id}.m3u8`; else drop with a logged count

**Deployment topologies:** gen-only (default compose) or gen + proxy (compose
`--profile proxy` or equivalent). When proxy is used, `proxy_base_url` in the
gen config must be the proxy address **AS REACHABLE BY JELLYFIN/FFMPEG**, since
it is embedded in M3U output and resolved by media clients, not by FASTGen.
There is no combined single-process mode. Sizing note: gen is a handful of HTTP
pulls and XML marshaling a few times daily (negligible); proxy is
network-I/O-bound byte shuttling (goroutine-per-connection + io.Copy;
bandwidth-limited, not CPU-limited).

**proxy_all mode (optional, off by default):** emits ALL channel URLs as proxy
URLs; the proxy answers NATIVE channels with a 302 to the upstream (zero media
bytes through the proxy) and fully rewrites BEACON channels. Justifications:
(a) observability — every playback start touches the proxy, enabling per-channel
now-playing/last-failure telemetry in the UI; (b) drift insulation — upstream
URL-format changes become proxy-internal fixes rather than M3U regeneration
events. Documented tradeoff: in this mode the proxy is availability-critical
for ALL channels (an outage breaks playback start even for healthy native
streams). Default remains selective proxying (BEACON only).

## What FASTGen serves

- `GET /{provider}.m3u`  — M3U8 playlist for that provider
- `GET /{provider}.xml`  — XMLTV guide for that provider
- `GET /all.m3u` and `GET /all.xml` — merged output across enabled providers
- `GET /logos/{provider}/{channel_id}.png` — locally cached channel logos
- `GET /healthz` — JSON: per-provider status, last successful refresh, channel
  count, programme count, staleness flag
- `GET /metrics` — Prometheus format (refresh duration, failures, channel counts)

## Providers (initial set; architecture must make adding more trivial)

Implement a `Provider` interface (Fetch() → normalized []Channel + []Programme)
with two fetch strategies:

### Direct API adapters

**LG Channels US** — `GET https://api.lgchannels.com/api/v1.0/schedulelist`
with headers `x-device-country: US`, `x-device-language: en`, and a desktop
Chrome user-agent. Response JSON: `categories[] → channels[] → programs[]`.
Channel fields: `channelId`, `channelName`, `channelNumber` (string!), 
`channelLogoUrl`, `mediaStaticUrl` (strip query string), `providerId`,
`channelGenreName`. Programs: `programTitle`, `description`, `startDateTime`/
`endDateTime` (ISO8601 Z → XMLTV `20060102150405 +0000`). Dedupe channels by
`channelId` across categories.

### i.mjh.nz adapters (matthuisman's aggregated feeds)

Base: `https://raw.githubusercontent.com/matthuisman/i.mjh.nz/master/{Provider}/`

- channels: `.channels.json.gz` → `{slug, regions: {us: {channels: {id: {name,
  chno, logo, group, description}}}}, headers}`
- EPG: `{region}.xml.gz` — already valid XMLTV; filter to emitted channels only
- Stream URL: `https://jmp2.uk/{slug-with-id-substituted}` where slug comes from
  the metadata `slug` field (e.g. Samsung = `stvp-{id}`)

**GOTCHA:** PlutoTV's `.channels.json.gz` has NO `slug` field. Its slug must be
configurable per provider and default to `plu-{id}.m3u8` for Pluto. Never emit a
bare `{id}.m3u8` fallback silently — log a warning if slug is missing and no
override exists.

Initial mjh providers: PlutoTV (us), SamsungTVPlus (us), Roku.

**GOTCHA (Roku):** Roku's `.channels.json.gz` has NO `regions` wrapper --
channels live at the top level -- and no `slug` field (use `rok-{id}.m3u8`,
WITH extension). Its `group` is a LIST (`groups`); take the first element.
Its EPG file is `all.xml.gz`, not `{region}.xml.gz`. The mjh adapter must
handle both shapes (regioned + regionless).

### Published-pair adapters (maintained upstream M3U + EPG consumed directly)

Third fetch strategy: some providers are best consumed via a community-
maintained, pre-generated M3U + XMLTV pair, re-emitted with our filtering,
labeling, numbering, and validation layered on top:

- **Xumo Play** (~400 ch): M3U + EPG from
  `https://raw.githubusercontent.com/BuddyChewChew/xumo-playlist-generator/main/playlists/xumo_playlist.m3u`
  and `.../xumo_epg.xml.gz`. Streams are direct CloudFront SSAI URLs. No
  upstream tvg-chno (synthesize).
- **DistroTV** (~170 ch): M3U + EPG from
  `https://raw.githubusercontent.com/vraomoturi/DistroTV/master/distrotv.m3u`
  and `.../distrotv.xml.gz` (~72h depth). Streams via Google DAI. Upstream
  tvg-ids CONTAIN SPACES (`dtv_EPGACE TV`) -- see id normalization. No
  tvg-chno (synthesize).
- **LocalNow** (local news/weather by market -- the only local content in the
  set): M3U `https://www.apsattv.com/localnow.m3u` + EPG
  `https://raw.githubusercontent.com/BuddyChewChew/localnow-playlist-generator/refs/heads/main/epg.xml`.
  CAUTION: playlist and EPG are maintained by different people; validate id
  match rate at refresh and surface it in health. apsattv may 403 datacenter
  IPs and non-browser UAs.

Published-pair requirements: parse EXTINF attributes + display name + stream
URL; dedupe by normalized tvg-id; sanitize the upstream EPG for bare
ampersands BEFORE parsing (upstream generators have shipped invalid XML);
rebuild the EPG through the XML marshaler so output validity is guaranteed
regardless of input.

**Direct-API candidates for later** (endpoints verified, adapters not yet
prioritized): Tubi (`https://tubitv.com/oz/epg/programming`; Fox library
channels), Plex (anon-token dance: `clients.plex.tv/api/v2/users/anonymous`
-> `epg.provider.plex.tv`; also available via mjh), TCL
(`gateway-prod.ideonow.com` / `tcl-channel-cdn.ideonow.com`).

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
- **Channel numbers:** per-provider integer `chno_offset` added to the native
  number when the upstream provides one. Emit as `tvg-chno` in M3U and
  `<lcn>` in XMLTV. Sort each playlist by final number. For providers with no
  upstream numbering (Xumo, DistroTV): `synthesize_chno: <base>` assigns
  sequential numbers -- but implement it properly with a PERSISTED id->number
  map (first-seen assignment, reused forever, stored in /data) so numbers
  survive upstream reordering; playlist-order assignment drifts.
- **Provider labels:** per-provider `label` (LG/Pluto/Samsung). Display name in
  M3U = `{name} · {label}`. `group-title` = `{label}: {group}`. Keep `tvg-name`
  as the clean unlabeled name.
- **Exclusion filters:** per-provider list of case-insensitive regexes matched
  against stream URL + provider id + channel name (fast first pass; the
  classifier probe is the authoritative gate). Filtered channels are removed
  from BOTH the M3U and the XMLTV.
- **Classifier probe details:** fetch master playlist -> first variant -> inspect
  first ~5 segment lines. BEACON if segments contain `/beacon/` or lack a media
  extension (.ts/.aac/.mp4/.m4s) before the query string. Fetch errors classify
  as NATIVE (never drop a channel on a transient network failure). Run probes
  concurrently (bounded worker pool). Amagi context: most FAST channels are
  played out by Amagi (`*.playouts.now.amagi.tv`); the beacon dialect is their
  SSAI ad-tracking format and appears across unrelated "providers" (BBC, A&E)
  because they share the same playout vendor.
- **DRM:** i.mjh.nz channel records with a `license_url` field are Widevine
  encrypted; drop them unconditionally (65 of Samsung's US channels at time of
  writing).
- **XML generation:** MUST use `encoding/xml` marshaling. Never build XML via
  string concatenation or templates — an upstream generator we replaced shipped
  unescaped `&` in `<display-name>` and it killed Jellyfin's entire guide parse.
  Structurally invalid output must be unrepresentable. Validate by re-parsing
  the serialized bytes before publishing to the cache.
- **M3U attribute values:** strip/replace double quotes.

## Logo caching (required, not optional)

Download every channel logo at refresh time into an on-disk cache
(`/data/logos/{provider}/{id}.{ext}`), and rewrite `tvg-logo` and XMLTV `<icon>`
to this service's own `/logos/...` URLs (base URL configurable, e.g.
`http://fastgen.lan:8180`). Rationale: Jellyfin fetches artwork itself and
chokes on hostile CDNs. Requirements:

- Fetch logos with per-provider request headers (mjh metadata's `headers` field,
  e.g. Samsung wants `user-agent: okhttp/4.12.0`).
- **Per-host TLS policy:** `tvpnlogopus.samsungcloud.tv` serves a chain rooted
  in DigiCert Global Root CA (G1), which Mozilla/Chrome distrusted 2026-04-15
  and which is absent from current system stores. Support per-host extra root
  CAs (PEM in config) and, as a last resort, per-host `insecure_skip_verify`
  for artwork-only hosts. Default everything else to system roots. Never apply
  relaxed TLS to stream or EPG endpoints.
- Cache is content-addressed enough to skip re-downloading unchanged logos
  (ETag/Last-Modified or just skip-if-exists with a max-age).
- Strip per-programme `<icon>` elements from mjh EPGs (thousands of them, they
  flood Jellyfin's pre-cache with fetch failures and barely render).

## Refresh & cache semantics (last-known-good, always)

- Per-provider refresh interval (default 6h) with ±10% jitter; independent
  goroutines; one provider failing never blocks others.
- A refresh is published only if it passes gates: channel count ≥ per-provider
  `min_channels`, XML re-parses, programme count > 0. Otherwise keep serving
  the previous good snapshot and mark the provider stale in `/healthz`.
- Snapshots are atomic swaps of in-memory objects (RWMutex or atomic.Pointer);
  additionally persist last-good output to `/data/cache/` so a container
  restart serves immediately even if upstreams are down at boot.
- HTTP responses carry `ETag` and `Last-Modified` from the snapshot; honor
  `If-None-Match`/`If-Modified-Since` with 304s (Jellyfin refetches often).

## Config

Single YAML file (`/data/config.yaml`), env var overrides for the basics
(`FASTGEN_LISTEN`, `FASTGEN_BASE_URL`, `FASTGEN_DATA_DIR`). Include a documented
default config in the repo covering everything above. Hot-reload on SIGHUP is
nice-to-have.

## Operational requirements

- Structured logging (slog), one line per refresh with counts and duration.
- Graceful shutdown; refresh contexts cancelled on SIGTERM.
- Timeouts on every outbound call (default 60s); retry with backoff (3 tries).
- No panics on malformed upstream data — skip the bad record, count it, log it.

## Docker

- Multi-stage build: `golang:1.23` builder → `gcr.io/distroless/static` (or
  scratch + tzdata + ca-certificates). `CGO_ENABLED=0`, static binaries.
  Produce **two images** (Dockerfile targets or separate Dockerfiles): `fastgen`
  and `fastproxy`. NOTE: images MUST include an up-to-date ca-certificates
  bundle; TLS trust is a first-class concern in this service.
- Volume on gen: `/data` (config, logo cache, last-good snapshots).
- Expose gen `8180` (and document proxy port, e.g. `8181`). Healthcheck hits
  `/healthz` on each service.
- Provide `docker-compose.yml`: `fastgen` required by default; `fastproxy`
  optional via a compose profile (e.g. `proxy`). Include comments showing the
  Jellyfin tuner/guide URLs to configure: tuner `http://fastgen:8180/lg.m3u`,
  guide `http://fastgen:8180/lg.xml`, etc., and how to set `proxy_base_url` when
  the proxy profile is enabled.

## Testing

- Unit tests for: normalization (offsets, labels, quote stripping), exclusion
  filters (must drop a channel whose stream URL contains `dinospluto-lgus`),
  XMLTV time conversion, the Pluto missing-slug default, and XML validity with
  hostile inputs (`A&E Crime 360`, names containing `<`, `"`).
- Golden-file tests for M3U and XMLTV output from fixture JSON.
- An integration test with `httptest` fake upstreams covering: failed refresh
  keeps last-known-good; min_channels gate refuses a gutted lineup; 304 flow.

## Web UI (required)

Embedded web UI **in the fastgen binary**: assets compiled via Go `embed` — no
Node runtime or separate frontend container. Server-rendered templates or a
small embedded SPA; keep it dependency-light. Backed by a JSON API (also usable
directly). Ship the UI foundation early and feather features as gen capabilities
land (classification, export reasons, health, config editor).

Core view — the channel table: every channel from every provider with a
classification badge and export status:
- NATIVE — exported, plays anywhere
- DRM — NOT exported; show the license_url as evidence
- BEACON — exported only when a proxy is configured; otherwise show an explicit
  "needs FASTProxy" state (never silently absent)
Filterable/searchable by provider, status, name, channel number.

Per-channel detail: upstream URL vs emitted URL, last probe time and result,
the precise reason string for any filter decision (regex hit, DRM, beacon
without proxy, min_channels gate), per-channel force-enable/disable override.

Provider rollups mirroring /healthz: last refresh, channel/programme counts,
staleness, drop counts by reason.

Config editing for the iterated-on settings (exclusion regexes, chno offsets,
labels, proxy settings, per-channel overrides), persisted to the YAML config.

Acceptance test for the UI: a user wondering why a specific LG channel is
missing from Jellyfin can locate it in the table and read a one-line reason in
under ten seconds.

## FASTProxy requirements

- Separate `cmd/fastproxy` binary and Docker image (not a gen mode flag).
- Endpoints: `/stream/{provider}/{id}.m3u8` (master), rewritten variant paths,
  `/seg/{token}` (segment shuttle), 302 redirects for NATIVE channels in
  proxy_all mode.
- Handle relative URL resolution in playlists, `#EXT-X-KEY` URI lines (rewrite
  those too), and preserve all other playlist tags byte-for-byte.
- Per-session upstream state keyed by client stream, TTL-expired; Amagi URLs
  embed session tokens that must be reused across a session's segment fetches.
- Never modify ad content: beacons are resolved (the tracking GET is made, the
  redirect followed) so impressions still count; the proxy translates dialect,
  it does not strip ads.
- Tests: golden rewrite tests from fixture playlists (beacon-style modeled on
  Amagi, clean, relative-URL, EXT-X-KEY); integration test proving a
  beacon-style fixture becomes a playlist whose every non-comment line targets
  the proxy.

## Additional test cases (from production failures)

- HEAD vs GET: health checks must use ranged GET; assert no HEAD requests are
  issued to stream endpoints.
- Classification migration: a channel flipping NATIVE->BEACON between refreshes
  must not change its emitted URL when proxy_all is on.

## Stream health validation (required; distinct from classification)

Classification (NATIVE/BEACON/DRM) answers "what kind of stream is this" and
decides export. Health answers "does it actually play right now" and is a
per-channel time-series. Keep them separate: health annotates by default and
gates export only by explicit opt-in.

**Probe depths (tiered):**
1. *Shape* — the classifier's playlist inspection (cheap, runs every refresh).
2. *Segment* — fetch the first media segment via ranged GET, verify real video
   (MPEG-TS sync byte 0x47 / fMP4 box header, size above a floor). Catches
   dead playouts, 403s, geo-blocks, empty segments.
3. *Decode* — run ffprobe against the stream URL; PASS requires non-null
   streams and format (the exact check Jellyfin performs). Ground truth.

**Default posture — passive first, active minimal.** Design constraint: many
installs must not collectively generate bot-fingerprint probe traffic or fake
ad impressions against free services. Therefore:
- Level 1 (shape): every refresh, all channels. No media fetched, no views.
- Level 2 (segment): daily, NATIVE channels only — a plain .ts GET fires no ad
  beacon. Never level-2 BEACON channels on a schedule (probing through the
  proxy fires impression beacons = fake views).
- Level 3 (ffprobe): OFF by default; opt-in config for users who accept the
  tradeoff. Bounded worker pool, per-probe timeout, jitter over a configurable
  window (default 60m), randomized order — never a clockwork fingerprint.
- **Passive health (primary signal):** FASTProxy records the outcome of every
  REAL playback session (upstream playlist fetch result, segment flow,
  failure class) and feeds the health state machine. Watched channels are
  continuously validated by actual use at zero extra requests; unwatched
  channels honestly remain UNTESTED.
- **On-demand probe:** a "Test now" action in the UI channel detail runs one
  level-3 probe for that single channel — indistinguishable from a viewer
  tuning in. This is the sanctioned way to deep-test BEACON channels.

**Health event log:** persist probe results and (once FASTProxy lands)
playback outcomes as an append-only event log with a versioned schema —
`(schema_version, channel_id, source, outcome, failure_class, coarse
timestamp)`. The state machine and UI probe history are derived views over
this log. Schema changes must be versioned and backward-readable across
upgrades.

**HealthSource seam:** define health input as an interface; probe results and
playback telemetry are its two implementations.

**Probe paths:** on-demand and opt-in probes test BEACON channels THROUGH the
proxy URL (end-to-end validation of the rewrite chain); NATIVE channels at
their emitted URL.

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
