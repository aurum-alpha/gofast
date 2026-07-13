# AGENTS.md — GoFAST

Instructions for humans and coding agents working in this repository.

## Source of truth

- Product requirements: `docs/SPEC.md`
- Architecture / build approach: `docs/ARCHITECTURE.md`
- Work queue: Linear team **J27**, project **GoFAST**  
  https://linear.app/aurum-alpha/project/gofast-7332d71ee889/overview

Implement **one Linear issue at a time**. Do not invent parallel workstreams from the plan file.

## Branch and pull request rules

1. **One Linear task per pull request.** Do not combine multiple issues in one PR.
2. **Branch name** must include the Linear issue id, e.g.:
   - `j27-1-persist-spec`
   - `J27-1-persist-spec`
3. Open the PR against `main` (unless the issue says otherwise). Link the Linear issue in the PR description.

## Quality gates (before commit and before push)

1. **Automated tests must pass** before commit:
   - `go test ./...`
   - Any issue-specific checks called out in the Linear acceptance criteria
2. **Agent smoke checks** (optional, on the branch): run quick local verification to catch obvious breakage before handing off.
3. Do not commit or push with failing tests.
4. Do not use `--no-verify` to skip hooks.

## Human approval gate (required)

**Do not commit, push, merge, or mark a Linear issue Done until the human has manually tested and given feedback.**

Workflow for agents:

1. Implement on a branch; leave changes **uncommitted** or **committed locally only** until the human confirms — ask if unclear.
2. Post a short handoff: what changed, exact commands to run, expected results.
3. Wait for explicit human sign-off (e.g. “looks good”, “merge it”, “commit and push”).
4. Only after sign-off: commit (if needed), push, open/update PR, merge if requested, set Linear to **Done**.

Agents may set Linear to **In Progress** while coding and **In Review** when a PR is ready for human testing. Never skip straight to **Done** on agent-only verification.

## Linear status workflow

Keep the issue status honest as you work:

| When | Status |
|------|--------|
| Not started | Backlog |
| Next up / ready to pull | Todo |
| Actively coding on the branch | In Progress |
| Ready for human manual test / PR open | In Review |
| Human verified; merged or accepted | Done |

Update the Linear issue as you go (short comments on blockers, verification notes, PR link).

## Milestone order

Honor Linear **project milestones** M0 → M5. Do not start issues in milestone N+1 until milestone N exit criteria are met (tests green, milestone issues Done), unless the issue is explicitly unblocked and parallel-safe per its blocker list.

## Product boundaries

- **fastgen** is the primary product (M3U/XMLTV, UI, health).
- **fastproxy** is an optional add-on (separate binary and Docker image).
- Prefer extending shared `internal/` packages over duplicating logic.

## Config (Twelve-Factor)

		Follow [12factor.net/config](https://12factor.net/config) for deploy-varying configuration:

1. **Strict separation of config from code.** Anything that changes between deploys (listen address, public `base_url`, data directory, credentials, backing-service URLs) must not be hardcoded and must not be committed as a real deploy file.
2. **Environment variables are the primary interface** for per-deploy values. Prefer vars that Portainer/`stack.env` can set without rebuilding images.
3. **Shared listen port:** both `fastgen` and `fastproxy` use **`PORT`** (Heroku/Cloud Run style: `8180` or `:8180`). Each container sets its own value; a shared Dockerfile does not prevent this — env is per image/runtime, and the two compose services never share a process environment.
4. **Service-prefixed only when needed:** gen-only settings stay `FASTGEN_*` (`FASTGEN_BASE_URL`, `FASTGEN_DATA_DIR`, `FASTGEN_CONFIG`). Compose *host* publish ports (`FASTGEN_HOST_PORT`, `FASTPROXY_HOST_PORT`) stay prefixed because they live in the same `stack.env` file.
5. **Litmus test:** the repo could be made public without leaking credentials or private hostnames.
6. **No named environment bundles** in code (`development` / `staging` / `production` switches). Each deploy gets an independent set of env vars.
7. **Optional YAML on the data volume** (`/data/config.yaml`) may hold structured, non-secret app settings (providers, exclusions, etc.) that are awkward as flat env. That file is **runtime data**, not source: ship `config.example.yaml` as documentation only; never commit a filled production `config.yaml`.
8. **Precedence:** defaults → YAML file (if present) → **environment** (env always wins for overlapping keys).
9. **Secrets** only via env (or a secret store injected as env)—never in git, never in example files as real values.
