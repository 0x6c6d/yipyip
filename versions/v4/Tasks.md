# Tasks v4: yipyip (skills, jev decisions, mouse-friendly input and tabs)

_Generated from versions/v4/plan.md. This file is the loop's persistent state (status and notes
only): update it in place, never regenerate it during a build. Briefs live in `versions/v4/tasks/<ID>.md`,
verifications in `scripts/verify/v4/<ID>.sh`. IDs are unique within this version._

## Standing constraints

The supervisor prepends these, verbatim, to every worker brief:

- `make check` must pass for the whole repo (`gofmt -l .` prints nothing, `go vet ./...`, `go test ./...`, `go build ./...`).
- Protected (never modify): `Plan.md`, `README.md` (sections 1–9; only a docs task may append a `## Usage` section at the end), `prompts/`, `.claude/`, `versions/`, `scripts/verify/`, the "Delegation workflow" section of `CLAUDE.md`.
- Security-critical files `internal/provider/transport.go`, `internal/tools/pathguard.go`, `internal/tools/bash.go`, `internal/agent/approval.go`, `internal/config/redact.go` are changed only by a task whose brief names them; all other code calls them and never re-implements host checks, path confinement, exec, approval or redaction.
- `go.mod` and `go.sum` stay unchanged against the baseline commit `4fabda9` (no new modules; standard library and the existing dependencies only).
- Offline only: tests use the `httptest` fakes in `internal/provider/providertest` and temp dirs (`t.TempDir()`, temp HOME); no test or Makefile target contacts a real host (never `api.typesafe.ai`, never OpenRouter); no test needs `TYPESAFE_API_KEY` or any real secret. The only host a worker may contact is `proxy.golang.org` (via `go mod download`).
- Non-test code never uses `http.DefaultClient`, `http.DefaultTransport` or `http.Get`/`Post`/`Head`; `os/exec` only in `internal/tools/bash.go` and `internal/tui/clipboard.go`.
- Secrets: every text shown in the UI, sent to a model or jev, or written to a file passes the config Redactor; the test secret `SECRET123` must never appear in a view, request body or written file.
- No commits, no `git init`/`reset`/`stash`/`checkout` of other revisions; leave changes in the working tree for the supervisor.
- Test names are exactly those in the brief (top-level `func TestXxx`); TUI tests drive `Model.Update` / `Model.Command` and `View()` (no teatest); every new test must finish in well under 10 s.

## Task List

| ID | Phase | Title | Depends-on | Model | Status | Notes |
|----|-------|-------|------------|-------|--------|-------|
| T000 | 0 | Preflight | — | supervisor | todo | |
| T101 | 1 | Skills loader, project dir (internal/skills) | T000 | opus | todo | |
| T102 | 1 | /<skill> args dispatch in the TUI | T101 | sonnet | todo | |
| T201 | 2 | Skills loader: global dir, override, skips | T101 | opus | todo | |
| T202 | 2 | Skill run: built-in wins, redaction, warnings | T102, T201 | opus | todo | |
| T203 | 2 | /skills command and / suggestions | T202 | opus | todo | |
| T204 | 2 | e2e TestSkillE2E | T203 | sonnet | todo | |
| T301 | 3 | Jev config block (defaults, route validation) | T000 | opus | todo | |
| T302 | 3 | Fake System One endpoint in providertest | T000 | sonnet | todo | |
| T303 | 3 | Jev client (internal/provider/jev.go) | T302 | opus | todo | |
| T304 | 3 | Jev host in the allowlist; session jev client | T301, T303 | opus | todo | |
| T401 | 4 | Jev decider core (toggle, ask, timeout, stats) | T304 | opus | todo | |
| T402 | 4 | Jev answers single-choice questions | T401 | opus | todo | |
| T403 | 4 | Jev turn routing (easy/hard per turn) | T401 | opus | todo | |
| T404 | 4 | Risk-screen hook in the approval gate | T000 | opus | todo | |
| T405 | 4 | Jev risk screen wired into the session | T401, T404 | opus | todo | |
| T406 | 4 | /jev command, /jev stats and status line | T402, T403 | sonnet | todo | |
| T407 | 4 | e2e TestJevE2E | T402, T403, T405, T406 | sonnet | todo | |

Status values: `todo` · `done` · `blocked` · `superseded`. No pipe characters inside cells.

## Execution Protocol

Run with the `loop-coding` skill (`/loop-coding v4`). The authoritative procedure
(round limit, commit format, Definition-of-Done repair) is in
`.claude/skills/loop-coding/SKILL.md`; in short, per iteration:
1. Select the first `todo` row whose dependencies are all `done`.
2. Read `versions/v4/tasks/<ID>.md`; dispatch one fresh worker with the row's `Model` and a brief made of the
   Standing constraints, the task's Brief and the path of its verification script.
3. The supervisor runs `scripts/verify/v4/<ID>.sh` itself (incl. its negative checks); never trust
   the worker's claim.
4. Pass → one commit with the work and this file's update. Fail → feedback; still failing
   after 3 rounds → `blocked`.
5. Nothing selectable → run the Global Definition of Done of `versions/v4/plan.md`; done when it passes.

## Changelog

- 2026-10-22: batch 1 of 3 generated (phases 0–2: T000, T101–T102, T201–T204). Phases 3–4 and 5–6 follow as later batches appended to this table.
- 2026-10-22: batch 2 of 3 generated (phases 3–4: T301–T304, T401–T407); rows appended after T204, earlier rows unchanged. Phases 5–6 follow as batch 3.
