# GoFAST

Self-hosted FAST channel aggregator for Jellyfin.

- **fastgen** — primary service: channel lineups, EPG (M3U + XMLTV), logos, health, embedded UI
- **fastproxy** — optional add-on: dialect translation for Amagi SSAI beacon rewrite and Google DAI SESSION mint

## Docs

- [AGENTS.md](AGENTS.md) — how to work in this repo (Linear, branches, PRs, quality gates)
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — dual-binary design, UI feathering, milestones
- [docs/SPEC.md](docs/SPEC.md) — detailed product requirements and gotchas
- [docs/TERMINOLOGY.md](docs/TERMINOLOGY.md) — glossary (HLS, SSAI, dialects, mint, proxy URLs)

## Linear

Project: [GoFAST](https://linear.app/aurum-alpha/project/gofast-7332d71ee889/overview) (team **J27**)

Implementation proceeds **one Linear issue at a time**.

## Run with Docker

Images are published to GHCR on every merge to `main` (after UI build + compile + test pass):

| Service | Image tags |
|---------|------------|
| fastgen | `…/fastgen:latest`, `…/fastgen:build-N`, `…/fastgen:sha-<short>` |
| fastproxy | `…/fastproxy:latest`, `…/fastproxy:build-N`, `…/fastproxy:sha-<short>` |

`N` is the GitHub Actions run number (monotonic). Pin Portainer / `stack.env` with `IMAGE_TAG=build-N`. Running build identity is on `GET /healthz` (`version.build` / `version.commit`) and Status → System.

Default ports: **8180** (gen), **8181** (proxy). With the optional nginx edge: **80/443**.

### Topologies

| Mode | Compose | Playback |
|------|---------|----------|
| **Gen-only** (default) | `docker compose up` | NATIVE / Xumo SSAI direct; Amagi SSAI + SESSION filtered until proxy is configured |
| **Gen + proxy** | `docker compose --profile proxy up` (or `COMPOSE_PROFILES=proxy`) | Set `FASTGEN_PROXY_BASE_URL` to the proxy origin **reachable by Jellyfin/ffmpeg** (embedded in M3U). Proxy uses internal `FASTPROXY_GEN_URL` (e.g. `http://fastgen:8180`) for origin pull + telemetry. Amagi → rewrite; SESSION → DAI mint then 302 |

**Three proxy-related URLs (do not conflate):**

| Knob | Who uses it | Typical local compose |
|------|-------------|------------------------|
| `FASTGEN_PROXY_BASE_URL` / `proxy_base_url` | Jellyfin/browser (M3U emit) | `http://localhost:8181` |
| `FASTGEN_PROXY_INTERNAL_URL` / `proxy_internal_url` | Gen health probes (Manual L2) rewrite | `http://fastproxy:8181` (compose default) |
| `FASTPROXY_GEN_URL` | Proxy → gen (origins + event push) | `http://fastgen:8180` |

`localhost` in `proxy_base_url` is fine for host players; gen inside Docker cannot dial that loopback for L2 — that is what `proxy_internal_url` fixes. Status/Proxy glass does not need gen→proxy; it uses proxy→gen events.

There is no single-process gen+proxy mode. Gen is light (periodic pulls + emit); proxy is network-I/O byte shuttling.

**`proxy_all` (optional, off by default):** emit every channel URL through the proxy. NATIVE / XUMO get a 302 to upstream (no media through proxy); Amagi is fully rewritten; SESSION is minted then 302’d to `stream_manifest`. Tradeoff: better playback observability and drift insulation, but the proxy becomes availability-critical for **all** channels. Default remains selective proxying (**Amagi + SESSION**).

**UI:** Status shows a compact proxy heartbeat glance; the **Proxy** tab is the detailed event glass. Playlist/origin failures also move channel health badges (`source=playback`).

### Homelab (pull published images)

Use [`docker-compose.prod.yml`](docker-compose.prod.yml) (pull-only; no build). The GitHub repo is private, so log in to GHCR once with a PAT that has `read:packages`:

```bash
echo YOUR_PAT | docker login ghcr.io -u YOUR_GITHUB_USERNAME --password-stdin

docker compose -f docker-compose.prod.yml --env-file stack.env pull
docker compose -f docker-compose.prod.yml --env-file stack.env up -d
curl http://localhost:8180/healthz
```

CI builds UI + binaries inside `node:22-bookworm` / `golang:1.26.5-bookworm` (same pins as local `Dockerfile`), then packages with [`Dockerfile.prod`](Dockerfile.prod) into GHCR.

### Portainer (homelab stack)

Production files (pull-only, no secrets):

| File | Purpose |
|------|---------|
| [`docker-compose.prod.yml`](docker-compose.prod.yml) | Portainer stack compose (pull GHCR; no `build:`) |
| [`stack.env`](stack.env) | Non-secret defaults (`IMAGE_TAG`, ports, optional bind path) |
| [`Dockerfile.prod`](Dockerfile.prod) | CI ship path only — not used on the homelab host |
| [`deploy/nginx/`](deploy/nginx/) | Optional edge nginx config + BYO TLS certs |

1. In Portainer → **Registries**, add `ghcr.io` with a GitHub PAT (`read:packages`).
2. Create a stack from `docker-compose.prod.yml` (services: `gen`, optional `proxy` / `edge`).
3. Paste or load `stack.env` as the stack environment variables.
4. Gen-only by default. Profiles via `COMPOSE_PROFILES`: `proxy`, `edge`, or `edge,proxy`.
5. Optional: set `FASTGEN_DATA=/path/on/host` for a bind mount instead of the named volume.
6. Set `FASTGEN_BASE_URL` to the public origin Jellyfin uses (include `:port` unless on 80/443; no trailing slash), e.g. `http://192.168.1.50:8180` or `https://gofast.example.com`.
7. When enabling the proxy profile, set `FASTGEN_PROXY_BASE_URL` to the public proxy origin Jellyfin/ffmpeg can reach, e.g. `http://192.168.1.50:8181` or `https://gofast-proxy.example.com`. Optionally set `FASTGEN_PROXY_ALL=true`.
8. Smoke test: `curl http://HOST:8180/healthz` → JSON with `"ok": true` plus per-provider stale/export fields (Docker/Portainer healthcheck only requires HTTP 200). Prometheus scrapes `GET /metrics` on the same port.

### HTTP endpoints

| Path | Purpose |
|------|---------|
| `/{id}.m3u` | Per-provider M3U playlist (`lg`, `pluto`, `samsung`, `roku`, `xumo`, `distrotv`, `localnow`) |
| `/{id}.xml` | Per-provider XMLTV guide |
| `/playlist.m3u` | Aggregate playlist across enabled providers |
| `/epg.xml` | Aggregate XMLTV across enabled providers |
| `/logos/{provider}/{file}` | Cached logos when `cache_logos` is on |
| `/` | Embedded operator UI |
| `/healthz` | Liveness + per-provider stale/export status |
| `/metrics` | Prometheus text exposition |

Channel detail includes an HLS **Preview** player (raw upstream vs emitted
playback URL). Prefer Emitted when the channel is via FASTProxy — proxy
`/stream` responses send CORS headers so the browser can audition them.

### Jellyfin Live TV (gen-only)

1. Bring the stack up (prod or local compose). Local compose auto-seeds `./.data/config.yaml` from [`config.example.yaml`](config.example.yaml) on first run (a `seed-config` init service), so all providers are enabled out of the box. In prod, copy [`config.example.yaml`](config.example.yaml) to `/data/config.yaml` before first boot; otherwise fastgen generates a defaults-only `config.yaml` with **no providers enabled**.
2. Set `FASTGEN_BASE_URL` to the origin **Jellyfin can reach** (LAN `http://HOST:8180` or HTTPS hostname behind nginx — see [TLS / nginx edge](#tls--nginx-edge-optional)). No trailing slash.
3. Wait until providers have last-known-good data: open the UI, or `curl …/healthz` and check per-provider `exported_channels` / `fetched_at`. Failed refreshes keep serving the previous M3U/XMLTV so Jellyfin is not left empty.
4. In Jellyfin: **Dashboard → Live TV → Tuners → Add** → type **M3U Tuner** → URL:
   - Aggregate: `http://HOST:8180/playlist.m3u` or `https://gofast.example.com/playlist.m3u`
   - Single provider: `…/lg.m3u` (etc.)
5. **Guide data** → add **XMLTV** → matching guide URL:
   - Aggregate: `http://HOST:8180/epg.xml` or `https://gofast.example.com/epg.xml`
   - Single provider: `…/lg.xml` (etc.)
6. Gen-only serves **NATIVE** and **Xumo SSAI** streams directly.
   **Amagi SSAI** (`AMAGI_SSAI`, legacy `BEACON`) and **SESSION** (Google DAI mint)
   need FASTProxy — see [docs/TERMINOLOGY.md](docs/TERMINOLOGY.md).
   (`COMPOSE_PROFILES` including `proxy` + `FASTGEN_PROXY_BASE_URL`). **DRM**
   stays dropped.

### TLS / nginx edge (optional)

Terminate TLS and do host-based routing in Docker. Bring your own PEM certs (no ACME/certbot in this stack). Fastgen does not invent absolute URLs from `Host` / `X-Forwarded-*` — always set `FASTGEN_BASE_URL` (and `FASTGEN_PROXY_BASE_URL` when using the proxy vhost) to the public HTTPS origin.

#### In-stack edge profile

Sample config: [`deploy/nginx/gofast.conf`](deploy/nginx/gofast.conf). Edit `server_name` values to your domains. Drop certs into the TLS dir (default [`deploy/nginx/certs/`](deploy/nginx/certs/)):

| File | Role |
|------|------|
| `fullchain.pem` | Certificate + chain |
| `privkey.pem` | Private key |

```bash
# stack.env
COMPOSE_PROFILES=edge
# or: COMPOSE_PROFILES=edge,proxy
FASTGEN_BASE_URL=https://gofast.example.com
# FASTGEN_PROXY_BASE_URL=https://gofast-proxy.example.com
EDGE_HTTP_PORT=80
EDGE_HTTPS_PORT=443
# EDGE_TLS_DIR=/path/on/host/certs
# EDGE_CONF=/path/on/host/gofast.conf

docker compose -f docker-compose.prod.yml --env-file stack.env up -d
```

- `edge` publishes **80/443** and proxies `gofast.example.com` → `gen:8180` (and optionally `gofast-proxy.example.com` → `proxy:8181`).
- Prefer Jellyfin → HTTPS hostnames. Gen/proxy host ports (`8180`/`8181`) can stay published for LAN debugging.

#### External nginx on the same Docker host

1. Keep the GoFAST stack on the compose network named `gofast` ([`docker-compose.prod.yml`](docker-compose.prod.yml)).
2. Attach your existing nginx container: `docker network connect gofast <nginx_container>` (or declare that network as `external: true` in the other compose).
3. Reuse the same upstream hostnames (`gen`, `proxy`) and `server_name` patterns as [`deploy/nginx/gofast.conf`](deploy/nginx/gofast.conf).
4. Publish **80/443** from only one stack — do not bind them twice on the host.

### Config (`/data/config.yaml`)

Runtime YAML on the gen data volume (not baked into the image). **Provider implementations are code** — each is a package compiled into the binary. This file only *customizes* a known provider (offsets, exclusions, enabled, URL overrides); it cannot add a provider without shipping Go, and ids with no implementation are ignored (warned) at startup. Implemented ids are `lg`, `pluto`, `samsung`, `roku`, `xumo`, `distrotv`, and `localnow`. Pluto/Samsung/Roku use Matt Huisman's `i.mjh.nz` feeds; Xumo/DistroTV/LocalNow consume maintained M3U/XMLTV pairs. Every provider (LG included) runs only when its YAML block is present; `enabled: false` disables an existing block. A fresh `/data` with no `config.yaml` generates a defaults-only file with no providers enabled.

`config.yaml` is **operator-writable** — mount `/data` read-write. If the file is missing on first boot, fastgen generates it from the baked-in code defaults (deploy-varying values stay in the environment). App-managed settings are persisted back atomically, preserving your comments and any keys fastgen does not manage (a `.bak` of the prior file is kept). A read-only mount surfaces a clear "config is read-only" message instead of failing.

**Settings UI (live, no restart):** the web UI's **Config** page edits settings as typed controls and applies them live — base/proxy URLs, logo caching, log level, all health knobs, the **Groups** taxonomy, and every per-provider setting including enable/disable (disable stops fetches, hides the channels, and 404s `/{id}.m3u`; the cache is kept so re-enabling is instant). The rule is: **edit in the UI = applies live; hand-edit the file = restart the container.** `listen`/`PORT` and `data_dir` are restart-only by design and have no UI control. Values set via environment variables show as locked in the UI (env always wins).

- [`config.example.yaml`](config.example.yaml) — starter template with the well-known provider overlays. Local compose auto-seeds it into `./.data/config.yaml` on first run (`seed-config` service); delete that file to re-seed. In prod, copy it to `/data/config.yaml` before first boot; otherwise a defaults-only file with no providers enabled is generated.

The selected last-known-good generation keeps exact upstream files under `/data/cache/{id}/generations/{generation}/raw/`: LG stores `schedule.json`; MJH providers store `channels.json.gz` + `guide.xml.gz`; published-pair providers store `playlist.m3u` plus `guide.xml.gz` (Xumo/DistroTV) or `guide.xml` (LocalNow). Published-pair refreshes log normalized playlist/guide ID match rates. Numberless channels use stable, persisted first-seen assignments from each provider's `synthesize_channel_numbers` base; removed IDs stay reserved.

Deploy-specific values (`PORT`, `FASTGEN_BASE_URL`, `FASTGEN_PROXY_BASE_URL`, `FASTGEN_PROXY_ALL`, `FASTGEN_CACHE_LOGOS`, …) stay in env — see `AGENTS.md`. `proxy_base_url` is the public FASTProxy origin; Amagi SSAI and SESSION channels are filtered with an explicit reason when it is absent. `proxy_all` defaults off.

**Logos:** `cache_logos` / `FASTGEN_CACHE_LOGOS` defaults **false** (upstream CDN URLs unchanged). When true (Config: “Cache + rewrite logos”), logos download under `{data_dir}/cache/{provider}/logos` and M3U/XMLTV/API — including per-channel emit `logo_url` — rewrite to `{FASTGEN_BASE_URL}/logos/...` (requires `base_url`). Soft artwork failures may leave a CDN URL until the next successful warm. Artwork-only TLS exceptions live under `artwork_tls` in YAML — they never apply to stream or EPG fetches.

### Local build from source

Full rebuild via [`Dockerfile`](Dockerfile) (Node + Go multi-stage — same image pins as CI):

```bash
docker compose build
docker compose up -d
```

Or build UI/Go on the host, then run the binary:

```bash
cd web && npm ci && npm run build && cd ..
go run ./cmd/fastgen
# open http://localhost:8180/
```

## Status

LG, Pluto, Samsung TV Plus, Roku, Xumo, DistroTV, and LocalNow provider pipelines are implemented. Additional product features continue through Linear issues.
