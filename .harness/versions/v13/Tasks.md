# Tasks v13: yipyip (global AGENTS.md)

_Generated from .harness/versions/v13/plan.md. This file is the loop's persistent state (status and notes
only): update it in place, never regenerate it during a build. Briefs live in `.harness/versions/v13/tasks/<ID>.md`,
verifications in `.harness/verify/v13/<ID>.sh`. IDs are unique within this version._

## Standing constraints

The supervisor prepends these, verbatim, to every worker brief:

- `make check` must pass for the whole repo (`gofmt -l .` empty, `go vet ./...`, `go test ./...`, `go build ./...`).
- Protected (never modify): the whole `.harness/` folder, `.claude/`, and the "Delegation workflow" section of `CLAUDE.md`. `README.md` and the rest of `CLAUDE.md` are changed only by the task that names them.
- Change nothing outside the files your task names (new test files in the named packages are fine). No changes in `internal/provider/`, `internal/tools/`, `cmd/yip/` in this version.
- No commits, no `git init`, no new dependencies (no edits to `go.mod`/`go.sum`). No network except `proxy.golang.org` via `go mod download`; tests run offline against the `httptest` fake provider in `internal/provider/providertest`.
- Non-test code never uses `http.DefaultClient`, `http.DefaultTransport` or `http.Get`/`Post`/`Head`; no new process starts; security logic stays in `internal/provider/transport.go`, `internal/tools/pathguard.go`, `internal/tools/bash.go`, `internal/agent/approval.go`, `internal/config/redact.go`, `internal/crashlog`, `internal/tools/reaper*.go` (call them, never re-implement them).
- Go 1.27, module `yipyip`; code is `gofmt`-formatted.

## Task List

| ID | Phase | Title | Depends-on | Model | Status | Notes |
|----|-------|-------|------------|-------|--------|-------|
| T000 | 0 | Preflight | — | supervisor | done | verify passed |
| T101 | 1 | Config.GlobalDir set by config.Load | T000 | opus | done | config.go, globaldir_test.go: GlobalDir set by Load |
| T102 | 1 | Global AGENTS.md reader and system prompt section | T101 | opus | done | prompt.go, session.go, globalagents_test.go: global section in system prompt |
| T103 | 1 | Session-level tests: Reset re-read, subagent prompt, /init project-only | T102 | sonnet | done | globalagents_session_test.go: Reset, subagent, /init tests |
| T201 | 2 | e2e: TUI, headless and subagent requests carry the global note | T102 | sonnet | done | e2e/helpers_test.go, globalagents_test.go; approval-prompt check is a weak substring |
| T202 | 2 | e2e: /new re-read and /init leaves the global file alone | T201 | sonnet | done | e2e/globalagents_test.go: /new, /clear, /init tests |
| T301 | 3 | /help mentions the global AGENTS.md | T000 | haiku | done | help.go, help_v13_test.go; skill_test.go expectation updated (help scrolls "/sessions" off) |
| T302 | 3 | README: global AGENTS.md | T102, T301 | sonnet | done | README.md: Instructions (AGENTS.md) subsection |
| T303 | 3 | CLAUDE.md architecture notes | T102, T202 | haiku | todo | |
| T999 | 3 | Definition of Done | T000, T101, T102, T103, T201, T202, T301, T302, T303 | supervisor | todo | |

Status values: `todo` · `done` · `blocked` · `superseded`. No pipe characters inside cells.

## Execution Protocol

Run with the `loop-coding` skill (`/loop-coding v13`). The authoritative procedure
(round limit, commit format, Definition-of-Done repair) is in
`.claude/skills/loop-coding/SKILL.md`; in short, per iteration:
1. Select the first `todo` row whose dependencies are all `done`.
2. Read `.harness/versions/v13/tasks/<ID>.md`; dispatch one fresh worker with the row's `Model` and a brief made of the
   Standing constraints, the task's Brief and the path of its verification script.
3. The supervisor runs `.harness/verify/v13/<ID>.sh` itself (incl. its negative checks); never trust
   the worker's claim.
4. Pass → one commit with the work and this file's update. Fail → feedback; still failing
   after 3 rounds → `blocked`.
5. Nothing selectable → run the Global Definition of Done of `.harness/versions/v13/plan.md`; done when it passes.
