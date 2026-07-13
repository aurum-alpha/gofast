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

Images are published to GHCR on every merge to `main`:

| Service | Image |
|---------|-------|
| fastgen | `ghcr.io/j27-aurum/gofast/fastgen:latest` |
| fastproxy | `ghcr.io/j27-aurum/gofast/fastproxy:latest` |

Default ports: **8180** (gen), **8181** (proxy).

### Homelab (pull published images)

The GitHub repo is private, so log in to GHCR once with a PAT that has `read:packages`:

```bash
echo YOUR_PAT | docker login ghcr.io -u YOUR_GITHUB_USERNAME --password-stdin
```

Then:

```bash
# gen only
docker compose pull
docker compose up -d
curl http://localhost:8180/healthz

# gen + proxy
docker compose --profile proxy pull
docker compose --profile proxy up -d
curl http://localhost:8181/healthz
```

Compose references the GHCR `image:` names and also keeps a local `build:` block, so `docker compose build` still works for development.

### Portainer (homelab stack)

Production files (pull-only, no secrets):

| File | Purpose |
|------|---------|
| [`docker-compose.prod.yml`](docker-compose.prod.yml) | Portainer stack compose |
| [`stack.env`](stack.env) | Non-secret defaults (`IMAGE_TAG`, ports, optional bind path) |

1. In Portainer → **Registries**, add `ghcr.io` with a GitHub PAT (`read:packages`).
2. Create a stack from `docker-compose.prod.yml`.
3. Paste or load `stack.env` as the stack environment variables.
4. Gen-only by default. To also run proxy, set `COMPOSE_PROFILES=proxy` in the stack env.
5. Optional: set `FASTGEN_DATA=/path/on/host` for a bind mount instead of the named volume.

### Local build from source

```bash
docker compose build
docker compose up -d
```

## Status

M0 bootstrap in progress (Docker stubs + GHCR publish). Application features land via Linear issues.
