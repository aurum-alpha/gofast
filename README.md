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

Implementation proceeds **one Linear issue at a time**. Next up: [J27-2](https://linear.app/aurum-alpha/issue/J27-2/002-dual-cmd-skeleton-httpx-slog-sigterm).

## Status

Phase 0 setup docs are in-repo; application code lands via Linear issues (next: [J27-2](https://linear.app/aurum-alpha/issue/J27-2/002-dual-cmd-skeleton-httpx-slog-sigterm)).
