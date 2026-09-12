# AGENTS.md — GoFAST

Instructions for humans and coding agents working in this repository.

This repository follows the Aurum Alpha agent standard:
https://github.com/aurum-alpha/workflows/blob/main/AGENTS.md
Rules below are additional to it, or state where this repository differs.
Nothing below restates it: a paragraph that could be pasted unchanged into
another repository belongs in the standard, not here.

## Source of truth

- Product requirements: `docs/SPEC.md`
- Architecture / build approach: `docs/ARCHITECTURE.md`
- Terminology: `docs/TERMINOLOGY.md`
- The version this ships: `.version` — a committed file, and the build stamps
  what it says. Never a tag first. (Arrives in the release pull request that
  cuts `1.0.0`; see **Version bumps**.)

## Work queue

**GitHub Issues** on [aurum-alpha/gofast](https://github.com/aurum-alpha/gofast/issues).
This is a public repository, and public repositories track work in GitHub Issues.

**Linear (team J27) is not in use here.** The GoFAST project still exists in
Linear and is legacy. Do not create or update Linear issues for this repository.

The branch name carries the issue number: `37-distrotv-session-mint`, or
`issue-37-distrotv-session-mint`.

## Commands

Verbatim, and the same commands CI runs — these are read off the shared jobs in
`aurum-alpha/workflows`, not reimplemented here. A gate you cannot reproduce
locally with one command is a defect in the gate.

Go, from the repository root:

```sh
test -z "$(gofmt -l .)"                                          # formatting
go vet ./...                                                     # vet
go test ./... -race -covermode=atomic -coverprofile=coverage/go-unit.out
go build ./...                                                   # compiles
```

Client, from `client/`:

```sh
pnpm typecheck                                  # tsc -b --noEmit
pnpm lint                                       # oxlint, --deny-warnings
pnpm format                                     # prettier --check .
pnpm test:unit                                  # vitest run, with coverage
pnpm build                                      # vite build
pnpm dev:client                                 # the Vite dev server
```

`pnpm format` checks; `pnpm exec prettier --write .` fixes. Why the fix command
is not a script, and why `.oxlintrc.json` and `.prettierrc.yaml` cannot be
edited here, are the
[developer-commands standard](https://github.com/aurum-alpha/workflows/blob/main/standards/015-commands.md)'s
to answer.

One linter now. eslint has been retired: `.oxlintrc.json` was written as a
translation of the eslint config it replaced, at the same rules and severities,
and the two were run side by side until they agreed finding for finding — which
is what let the retirement be a deletion rather than a migration.

**Both the oxlint and the formatting job are blocking here, with no
stabilization window.** So the ordinary rule applies: lint and format pass
before you commit. `--deny-warnings` is part of the command because the job runs
it that way, and a warning nobody must fix is a warning nobody reads.

Local development: `docker compose up` from the root; `pnpm dev:client` in
`client/` for the Vite dev server. There is no `pnpm dev` here: `dev` means
bring up the whole local stack, and in this repository that is compose, not a
`package.json` script.

## Quality gates

Before a commit, and again before a push:

- `test -z "$(gofmt -l .)"` and `go test ./...` for Go changes.
- The client's five commands when `client/` changed. None of them carries a
  stabilization window here.
- Any issue-specific check named in the acceptance criteria.

**Agent smoke checks are worth the minute.** Quick local verification on the
branch catches obvious breakage before the handoff, and this repository's
deliverables are two images an operator points a TV client at.

## Version bumps

**The fleet decides versioning semantics; this repository does not.** Read
[`standards/010-ci.md`](https://github.com/aurum-alpha/workflows/blob/main/standards/010-ci.md),
"One versioning implementation, and no repo defines its own" — when a release is
cut, how a build between releases names itself, and what may be emitted are
stated there and not restated here. What follows is only what is local.

`.version` is the source of truth, and it starts at `1.0.0`.

*The file itself is not in the pull request that wired all this up, and could
not have been: the version gate refuses a commit that moves `.version`
alongside anything but markdown, and the wiring is Go and YAML. It arrives in
the release pull request immediately after — `.version` and prose, nothing else
— which is the first thing this repository cuts under the rule and a working
demonstration of it.*

**A version bump is NOT part of the change. It is its own pull request.**
The version gate rejects a pull request that moves `.version` alongside
anything but markdown, so a commit carrying both is refused by name, with the
strays listed. Land the code first; cut the release afterwards in a pull request
touching `.version` and prose and nothing else. It also fails a version that
repeats, goes backward, malforms, or disappears, and it runs on pull requests
only — there is no base to compare against on a push — so it is skipped on every
merge to `main`.

**Main is therefore not versioned at every commit, and that is intended.** Work
merges without a version. Between releases every green build still publishes
`build-<run>` and `latest`, still uploads its artifacts, and still runs; the
binaries stamp a SemVer pre-release of the next patch (`1.0.1-dev.<sha7>`),
which sorts above the last release and below every release that could come next.
Those images are runnable. They are simply not minted.

**Two things here are named for a version, and both are gated on the version
actually having moved.** `version` and `version_changed` come from
the build; nothing re-derives them.

| Emission | Between releases | On a release commit |
| --- | --- | --- |
| Image `build-<run>`, `latest` | published | published |
| Image `v<version>` | **not applied** | applied |
| Binary `Version` ldflag | `1.0.1-dev.<sha7>` | `1.0.0` |

**An unstamped bench build says `dev`, and that is deliberate.**
`gha-runner-controller` and `lid-firmware` stamp `<version>-local` because each
has a build wrapper that can read the version file; this repo's bench path is a
plain `go build`, which has nothing to read it with. `dev` matches no version
anyone could have cut, so it cannot be mistaken for a release — the property the
`-local` suffix buys elsewhere. Do not add a Makefile to change this.

**Minting is wired through the catalog, not written here.** `ci.yml`'s
`release` stub calls the shared release job, the same implementation
`lid-firmware` and `gha-runner-controller` call: it cuts the git tag and the
GitHub release on exactly the commit that moved `.version`, reading `version`
and `version_changed` from the fastgen build and re-deriving neither. The
release attaches no binaries — this repository's deliverables are the two
images, and the versioned artifact is the `v<version>` tag the publish jobs
apply on the same commit.

Version increments follow semantic versioning:

- **PATCH** (`1.0.Y` → `1.0.Y+1`): bug fixes, internal refactors, test-only or
  doc-only changes, dependency bumps without behaviour change.
- **MINOR** (`1.X.y` → `1.X+1.0`): new capability — a new provider, endpoint,
  config key, or anything an operator would notice.
- **MAJOR** (`X.y.z` → `X+1.0.0`): a breaking change to the API contract, the
  config shape, or the emitted playlist/guide format.

A release pull request covering several merged issues takes the highest
applicable bump: one MINOR issue plus three PATCH issues is one MINOR bump.

## Approval

The standard's gate applies unchanged. This repository adds no earlier hold and
relaxes nothing.

The one local mechanic: after sign-off, merging is what closes the issue, via
`Fixes #N` in the pull request body. Do not close it by hand ahead of the merge.

## Issue status workflow

Keep the GitHub issue honest as you work:

| When | What to do |
|------|------------|
| Not started | Leave **open**; optional label `todo` / no assignee |
| Actively coding | Comment that work started; assign yourself if appropriate; open a draft PR early if useful |
| Ready for human manual test | Open/update PR; comment with verification steps; request review |
| Human verified; merged or accepted | Close via `Fixes #N` on merge, or close manually with a short note |

## Milestone order

Honor project milestones M0 → M5 (see `docs/ARCHITECTURE.md`). Do not start work in milestone N+1 until milestone N exit criteria are met (tests green, milestone work accepted), unless the issue is explicitly unblocked and parallel-safe.

**Vertical slice over adapter breadth:** ship one provider end-to-end (fetch → emit → HTTP playlist) before starting the next adapter epic.

## Conventions

What this repository does differently from the fleet, or holds itself to beyond
it. Each has its own section below:

| Convention | Section |
| --- | --- |
| What GoFAST is and is not responsible for | **Product boundaries** |
| How state is stored and migrated | **Persistence design** |
| Go style beyond `gofmt` and `vet` | **Idiomatic Go** |
| Configuration strictly from the environment | **Config (Twelve-Factor)** |
| Milestone ordering, M0 to M5 | **Milestone order** |
| How a version is cut, and what a build stamps | **Version bumps** |

## Product boundaries

- **fastgen** is the primary product (M3U/XMLTV, UI, health).
- **fastproxy** is an optional add-on (separate binary and Docker image).
- Prefer extending shared `internal/` packages over duplicating logic.

## Persistence design

- Prefer **one persistence pattern per data shape**. Channel labels that change over time (classification, health, …) use a shared **current + history** store keyed by provider and channel — not a new ad-hoc file or generation field per feature.
- Cache **generations** are for atomic **emit** of playlist/guide/raw, not the system of record for those labels.
- When a new feature shows an older store was the wrong pattern, **refactor into the shared pattern** rather than bolting on a parallel layer to keep a ticket artificially small.

## Idiomatic Go

Write Go the way Go is written — not enterprise-layer theater.

**Domain vs guts**
- Domain types in `internal/model` use product language (`Provider`, `Channel`, `Programme`). Keep the domain pure: product nouns live there, not in adapter packages.
- Adapter/port interfaces use abstract capability/`-er` names (`provider.Reader`, later `Store`, etc.). Do not steal domain nouns for ports (`provider.Provider` is wrong when `model.Provider` is the config entity).
- Domain nouns that happen to end in `-er` (e.g. `Provider`) are still nouns. Interface `-er` names are verbs/capabilities; package qualification (`provider.Reader` vs `io.Reader`) is how Go avoids clashes — many packages define their own `Reader`.

**Behavior lives with the type**
- Self-contained business logic that validates or mutates a domain value belongs as **methods on that type** (e.g. `Channel.Normalize`, `MatchesExclusion`), or as package funcs in the same domain package when they are pure helpers over those types.
- Do **not** invent parallel utility packages (`normalize`, `helpers`, `utils`) whose only job is to operate on `model` types. If the code needs only the model, it belongs in `model`.
- **Exception — presentation formatting:** playlist/guide string shaping (M3U attrs, display names, group titles) lives in `internal/format` (`StripQuotes`, `FormatDisplayName`, `FormatGroupTitle`). That package takes plain strings, not domain methods; domain mutation stays on `model`.
- If a function’s first argument is `*T` / `T` and it only operates on that value, make it a **method** (`(c *Config) merge(o *Config)`), not `merge(cfg, o *Config)`.
- Constructors are `New` / `NewXxx` returning `(*T, error)` when they load or assemble state (e.g. `config.New(path)`: start from defaults, merge a YAML overlay, merge an env overlay). Keep overlay helpers unexported when only `New` uses them.

**Prefer the stdlib**
- Use stdlib types when they already satisfy the need (e.g. `time.Duration` with yaml.v3 duration strings). Do not wrap them for “fancy” YAML/input shapes we do not need.

**Serialization belongs on the domain**
- Put `json` / `yaml` tags on domain types in `internal/model`. Those tags *are* the wire shape (what NestJS would call a DTO) — do not invent parallel `FooView` / DTO structs that mirror the model. This means **our** config/API surface, not every external file format.
- Spell out Go identifiers (`ChannelNumberOffset`, `SynthesizeChannelNumbers`). Config/API tags use the same clear snake_case (`channel_number_offset`, `synthesize_channel_numbers`) unless an **external** format requires a specific name (e.g. M3U `tvg-chno`, XMLTV `<lcn>`). Do not keep abbreviated `chno_*` keys in *our* YAML/JSON just because M3U uses `tvg-chno`.
- Use `MarshalJSON` / `UnmarshalJSON` on the domain type only when the stdlib codec cannot express the field (e.g. `time.Duration` as a Go duration string in JSON).
- Keep API payloads single-purpose. Do not stuff unrelated server/runtime config (`listen`, `path`, `data_dir`) into a providers response — separate endpoints or log-only fields.
- **External playlists:** M3U and XMLTV live in `internal/m3u` and `internal/xmltv` with private wire types — not `MarshalXML` on `model.Channel` / `model.Programme`, and not a grab-bag `emit` package.

**Packages and files**
- Prefer extending shared `internal/` packages over duplicating logic.
- Concrete adapters (`internal/provider/lg`) expose idiomatic constructors (`New` → `*Client`) that implement the port interface.
- Prefer **many small files** over large grab-bags. One concern per file when practical (e.g. `healthz.go` for the health handler, `providers.go` for provider API routes). Same package, split by responsibility — not one god file.

**Type and methods share one file**
- **Never** define methods on a type in a different file from the type’s definition. The struct/type and all of its methods live together (e.g. `Channel` + methods in `channel.go`, not a grab-bag `model.go` plus `channel.go` methods).
- Within that file, order methods: **value receivers first** (alphabetical by method name), then **pointer receivers** (alphabetical). Package-level helpers for that type follow after the methods.
- Apply this everywhere, especially `internal/model`.

**gofmt**
- When editing Go, **write/save the full file** (not a tiny patch hunk) so the editor/`gofmt` runs on the whole file and keeps alignment (struct tags, etc.).
- CI fails if any `.go` file is not `gofmt`-clean (`gofmt -l .` must be empty). Locally: `gofmt -w .` or rely on format-on-save.

## Config (Twelve-Factor)

Follow [12factor.net/config](https://12factor.net/config) for deploy-varying configuration:

1. **Strict separation of config from code.** Anything that changes between deploys (listen address, public `base_url`, data directory, credentials, backing-service URLs) must not be hardcoded and must not be committed as a real deploy file.
2. **Environment variables are the primary interface** for per-deploy values. Prefer vars that Portainer/`stack.env` can set without rebuilding images.
3. **Shared listen port:** both `fastgen` and `fastproxy` use **`PORT`** (Heroku/Cloud Run style: `8180` or `:8180`). Each container sets its own value; a shared Dockerfile does not prevent this — env is per image/runtime, and the two compose services never share a process environment. Prefer keeping in-container `PORT` at the image default and varying only compose **host** publish ports (`FASTGEN_HOST_PORT` / `FASTPROXY_HOST_PORT`) so HEALTHCHECK URLs stay aligned.
4. **`FASTGEN_BASE_URL`:** the public origin Jellyfin/browsers use to reach gen (logos, absolute links). Include the port when clients are not on 80/443 — e.g. `http://192.168.1.50:8180` or `http://fastgen.lan:8180`. Behind a reverse proxy on HTTPS use `https://gofast.example.com` (no port). No trailing slash. This is independent of `PORT` / `FASTGEN_HOST_PORT`.
5. **Service-prefixed only when needed:** gen-only settings stay `FASTGEN_*` (`FASTGEN_BASE_URL`, `FASTGEN_DATA_DIR`, `FASTGEN_CONFIG`, `FASTGEN_PROXY_BASE_URL`, `FASTGEN_PROXY_INTERNAL_URL`, `FASTGEN_PROXY_ALL`, `FASTGEN_CACHE_LOGOS`, `FASTGEN_REGIONS`). Compose *host* publish ports stay prefixed because they live in the same `stack.env` file.
6. **Litmus test:** the repo could be made public without leaking credentials or private hostnames.
7. **No named environment bundles** in code (`development` / `staging` / `production` switches). Each deploy gets an independent set of env vars.
8. **Optional YAML on the data volume** (`/data/config.yaml`) holds structured, non-secret app settings. **Provider implementations are code** — each provider is a Go package (e.g. `internal/provider/lg`) exposing `New` + `DefaultSettings`, wired into a `map[id]provider.Reader` in the bootstrap. YAML `providers.<id>` only *overlays settings* for a known provider (enabled, label, offset, exclusions, URL overrides); an id with no implementation is ignored (warned) at startup. Adding a provider means shipping Go, not editing YAML. Copy `config.example.yaml` to `/data/config.yaml` to customize. That file is **runtime data**, not source: never commit a filled production `config.yaml`.
9. **Config grows with features — do not pre-land knobs.** Only add YAML/env keys in the same PR that implements the feature that reads them (e.g. `proxy_base_url` with emission, logo TLS with logo cache, health schedules with probes). Extend `config.example.yaml` in that same PR. Do not open standalone “config layer” work ahead of the feature.
10. **Precedence:** defaults → YAML file (if present) → **environment** (env always wins for overlapping keys).
11. **Secrets** only via env (or a secret store injected as env)—never in git, never in example files as real values.
