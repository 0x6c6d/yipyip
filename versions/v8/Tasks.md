# Tasks v8: yipyip (remove jev, web_fetch)

_Generated from versions/v8/plan.md. This file is the loop's persistent state (status and notes
only): update it in place, never regenerate it during a build. Briefs live in `versions/v8/tasks/<ID>.md`,
verifications in `scripts/verify/v8/<ID>.sh`. IDs are unique within this version._

## Standing constraints

The supervisor prepends these, verbatim, to every worker brief:

- `make check` must pass for the whole repo (`gofmt -l .` empty, `go vet ./...`, `go test ./...`, `go build ./...`).
- Protected (never modify): `Plan.md`, `README.md` (sections 1–9 always; the final `## Usage` section only when your brief says so), `versions/`, `scripts/verify/` (including the probes in `scripts/verify/v8/probes/`), `prompts/`, `.claude/`, the "Delegation workflow" section of `CLAUDE.md`, `go.mod`, `go.sum`, `internal/tools/pathguard.go`, `internal/tools/bash.go`, `internal/config/redact.go`. `internal/agent/approval.go` and `internal/provider/transport.go` change only in a task whose brief names them, and only as that brief says.
- Standard library only, no new modules. No `http.Client{`/`http.Transport{` literal outside `internal/provider/transport.go`; never `http.DefaultClient`, `http.DefaultTransport`, `http.Get`/`http.Post`/`http.Head` in non-test code.
- jev is being removed: never add the letters `jev`, `systemone` or `typesafe` (any case) to a `.go`, `.json` or `Makefile` file, tests included; where a test needs the old config key or command name, build it as `"\x6aev"` in Go.
- Do not commit; do not run `git init`, `git stash`, `git checkout`, `git reset` or `git clean`. No network access except `proxy.golang.org` via `go mod download` (nothing new is needed). Tests are offline: the fake provider in `internal/provider/providertest` and local `httptest` servers on 127.0.0.1 only; never a real host or a public IP address.
- Do not run the verify scripts of v1–v7 (they pin pre-v8 state and are not rerun). Run the verification script named in your brief yourself before reporting; never edit it.

## Task List

| ID | Phase | Title | Depends-on | Model | Status | Notes |
|----|-------|-------|------------|-------|--------|-------|
| T000 | 0 | Preflight | — | supervisor | done | preflight passes |
| T101 | 1 | Remove jev from internal/tui and e2e | T000 | opus | done | jev removed from tui and e2e |
| T102 | 1 | Remove jev from internal/agent (main turn on the session model) | T101 | opus | done | jev removed from agent |
| T103 | 1 | Remove jev from internal/provider and providertest | T102 | opus | done | jev removed from provider |
| T104 | 1 | Remove jev from internal/config (leftover key ignored) | T103 | opus | done | jev removed from config, leftover key ignored; row fixed: grep skips .yipyip |
| T199 | 1 | Phase 1 exit (no jev in code) | T101, T102, T103, T104 | supervisor | done | phase 1 exit passes |
| T201 | 2 | Remove the jev block from the README Usage section | T000 | haiku | done | README jev block removed |
| T202 | 2 | Remove jev from docs/usage.md | T000 | haiku | done | usage.md jev removed |
| T203 | 2 | Remove jev from CLAUDE.md | T000 | haiku | done | CLAUDE.md jev removed |
| T301 | 3 | Fetch HTTP client NewFetchHTTPClient | T199 | opus | done | NewFetchHTTPClient in transport.go, fetch_test.go |
| T401 | 4 | Gate web_fetch in approval.go (one line) | T000 | opus | done | web_fetch gated in approval.go |
| T402 | 4 | HTML-to-text converter (stdlib) | T000 | opus | done | htmlToText converter |
| T403 | 4 | web_fetch tool: registration, URL check, request | T301, T401 | opus | done | web_fetch tool: registration, URL check, request |
| T404 | 4 | web_fetch tool: result (content types, 1 MiB, HTML, cut) | T402, T403 | opus | done | web_fetch result handling |
| T501 | 5 | Agent wiring: FetchClient seam, gated and registered tests | T401, T403 | opus | todo | |
| T502 | 5 | System prompt web sentence | T403 | sonnet | todo | |
| T503 | 5 | TUI approval prompt shows the web_fetch URL | T401, T403 | opus | todo | |
| T504 | 5 | e2e WebFetch tests | T404, T501 | opus | todo | |
| T601 | 6 | docs/usage.md: web_fetch section and network line | T202, T404, T501 | haiku | todo | |
| T602 | 6 | CLAUDE.md: web_fetch lines and traffic sentence | T203, T404, T501, T503 | haiku | todo | |
| T799 | 7 | Global Definition of Done | T199, T201, T202, T203, T301, T401, T402, T403, T404, T501, T502, T503, T504, T601, T602 | supervisor | todo | |

Status values: `todo` · `done` · `blocked` · `superseded`. No pipe characters inside cells.

## Execution Protocol

Run with the `loop-coding` skill (`/loop-coding v8`). The authoritative procedure
(round limit, escalation, commit format, Definition-of-Done repair) is in
`.claude/skills/loop-coding/SKILL.md`; in short, per iteration:
1. Select the first `todo` row whose dependencies are all `done`.
2. Read `versions/v8/tasks/<ID>.md`; dispatch one fresh worker with the row's `Model` and a brief made of the
   Standing constraints, the task's Brief and the path of its verification script.
   Rows with Model `supervisor` get no worker: the supervisor runs (or makes) what the task file describes itself.
3. The supervisor runs `scripts/verify/v8/<ID>.sh` itself (incl. its negative checks); never trust
   the worker's claim.
4. Pass → one commit with the work and this file's update. Fail → feedback; escalation and
   `blocked` as in the skill.
5. Nothing selectable → run the Global Definition of Done of `versions/v8/plan.md` (row T799); done when it passes.

Ordering notes: Phase 1 is a chain (tui → agent → provider → config) so that every commit builds and
`make check` stays green; T301 waits for T199 so the Phase 1 `transport.go` comment-only guard is checked before
fetch code is added; T401 gates `web_fetch` before T403 registers the tool, so no commit offers it ungated.
