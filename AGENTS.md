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
2. **Manual verification must pass** before commit **and** before push:
   - Exercise the acceptance criteria in the Linear issue
   - For Docker/UI issues: run the relevant compose/UI path and confirm behavior
3. Do not commit or push with failing tests or unmet manual checks.
4. Do not use `--no-verify` to skip hooks.

## Linear status workflow

Keep the issue status honest as you work:

| When | Status |
|------|--------|
| Not started | Backlog |
| Next up / ready to pull | Todo |
| Actively coding on the branch | In Progress |
| PR open, waiting on review/CI | In Review |
| Merged and acceptance met | Done |

Update the Linear issue as you go (short comments on blockers, verification notes, PR link).

## Milestone order

Honor Linear **project milestones** M0 → M5. Do not start issues in milestone N+1 until milestone N exit criteria are met (tests green, milestone issues Done), unless the issue is explicitly unblocked and parallel-safe per its blocker list.

## Product boundaries

- **fastgen** is the primary product (M3U/XMLTV, UI, health).
- **fastproxy** is an optional add-on (separate binary and Docker image).
- Prefer extending shared `internal/` packages over duplicating logic.
