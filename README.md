# GoFAST

Self-hosted FAST channel aggregator for Jellyfin.

- **fastgen** — primary service: channel lineups, EPG (M3U + XMLTV), logos, health, embedded UI
- **fastproxy** — optional add-on: HLS rewriting for Amagi SSAI beacon streams

## Docs

- [AGENTS.md](AGENTS.md) — how to work in this repo (Linear, branches, PRs, quality gates)
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — dual-binary design, UI feathering, milestones
- [docs/SPEC.md](docs/SPEC.md) — detailed product requirements and gotchas

## Linear

Project: [GoFAST](https://linear.app/aurum-alpha/project/gofast-7332d71ee889/overview) (team **J27**)

Implementation proceeds **one Linear issue at a time**.

## Run with Docker

Images are published to GHCR on every merge to `main` (after UI build + compile + test pass):

| Service | Image |
|---------|-------|
| fastgen | `ghcr.io/j27-aurum/gofast/fastgen:latest` |
| fastproxy | `ghcr.io/j27-aurum/gofast/fastproxy:latest` |

Default ports: **8180** (gen), **8181** (proxy).

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

1. In Portainer → **Registries**, add `ghcr.io` with a GitHub PAT (`read:packages`).
2. Create a stack from `docker-compose.prod.yml` (services: `gen`, optional `proxy`).
3. Paste or load `stack.env` as the stack environment variables.
4. Gen-only by default. To also run proxy, set `COMPOSE_PROFILES=proxy` in the stack env.
5. Optional: set `FASTGEN_DATA=/path/on/host` for a bind mount instead of the named volume.
6. Optional: set `FASTGEN_BASE_URL` to the public origin Jellyfin uses (include `:port` unless on 80/443), e.g. `http://192.168.1.50:8180`.
7. Smoke test: `curl http://HOST:8180/healthz` → `{"ok":true}` (container logs a `request` line; Docker/Portainer healthcheck uses the same path).

### Config (`/data/config.yaml`)

Runtime YAML on the gen data volume (not baked into the image). **Provider implementations are code** — each is a package compiled into the binary. This file only *customizes* a known provider (offsets, exclusions, enabled, URL overrides); it cannot add a provider without shipping Go, and ids with no implementation are ignored (warned) at startup. Implemented ids are `lg`, `pluto`, `samsung`, and `roku`. Pluto/Samsung/Roku use Matt Huisman's `i.mjh.nz` metadata/EPG feeds and run only when their YAML block is present; `enabled: false` disables an existing block.

- [`config.example.yaml`](config.example.yaml) — starter template with the well-known provider overlays; copy to `/data/config.yaml`.

Each enabled provider is served at `/{id}.m3u` and `/{id}.xml`; combined output is `/playlist.m3u` and `/epg.xml`. The selected last-known-good generation keeps exact upstream files under `/data/cache/{id}/generations/{generation}/raw/`: LG stores `schedule.json`, while MJH providers store `channels.json.gz` and `guide.xml.gz`.

Deploy-specific values (`PORT`, `FASTGEN_BASE_URL`, …) stay in env — see `AGENTS.md`. Config keys for proxy, logo TLS, and health are added later **with those features**, not as a standalone M0 layer. Adapters that fetch providers arrive in M1.

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

LG, Pluto, Samsung TV Plus, and Roku provider pipelines are implemented. Additional product features continue through Linear issues.
