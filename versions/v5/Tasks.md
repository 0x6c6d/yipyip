# Tasks v5: yipyip (jev routing only, click-to-cursor fix, longer subagent runs)

_Generated from versions/v5/plan.md. This file is the loop's persistent state (status and notes
only): update it in place, never regenerate it during a build. Briefs live in `versions/v5/tasks/<ID>.md`,
verifications in `scripts/verify/v5/<ID>.sh`. IDs are unique within this version._

## Standing constraints

The supervisor prepends these, verbatim, to every worker brief:

- `make check` must pass for the whole repo (`gofmt -l .` prints nothing, `go vet ./...`, `go test ./...`, `go build ./...`).
- Protected (never modify): `Plan.md`, `README.md` (sections 1–9; only the docs task may change the `## Usage` section at the end), `prompts/`, `.claude/`, `versions/`, `scripts/verify/`, the "Delegation workflow" section of `CLAUDE.md`.
- Security-critical files `internal/provider/transport.go`, `internal/tools/pathguard.go`, `internal/tools/bash.go`, `internal/agent/approval.go`, `internal/config/redact.go` are changed only by a task whose brief names them; all other code calls them and never re-implements host checks, path confinement, exec, approval or redaction.
- `go.mod` and `go.sum` stay unchanged against the baseline commit `0c3a7c5` (no new modules; standard library and the existing dependencies only).
- Offline only: tests use the `httptest` fakes in `internal/provider/providertest` (fake provider, fake System One endpoint `providertest.NewJev`) and temp dirs (`t.TempDir()`, temp HOME); no test or Makefile target contacts a real host (never `api.typesafe.ai`, never OpenRouter); no test needs `TYPESAFE_API_KEY` or any real secret. The only host a worker may contact is `proxy.golang.org` (via `go mod download`).
- Non-test code never uses `http.DefaultClient`, `http.DefaultTransport` or `http.Get`/`Post`/`Head`; `os/exec` only in `internal/tools/bash.go` and `internal/tui/clipboard.go`.
- Secrets: every text shown in the UI, sent to a model or jev, or written to a file passes the config Redactor; the test secret `SECRET123` must never appear in a view, request body or written file.
- No commits, no `git init`/`reset`/`stash`/`checkout` of other revisions (reading old revisions with `git show <rev>:<path>` is allowed); leave changes in the working tree for the supervisor.
- Test names are exactly those in the brief (top-level `func TestXxx`); TUI tests drive `Model.Update` / `Model.Command` and `View()` (no teatest); every new test must finish in well under 10 s.
- v4 tests that assert behaviour this version removes or changes (jev answering questions, the jev risk screen, the old `JevStats` fields, two-tier routing) are updated or deleted by the task whose brief says so, and only there.

## Task List

| ID | Phase | Title | Depends-on | Model | Status | Notes |
|----|-------|-------|------------|-------|--------|-------|
| T000 | 0 | Preflight | — | supervisor | done | verify T000.sh 10/10 |
| T101 | 1 | Config: three-tier jev.route, only hard required | T000 | opus | done | internal/config/jev.go + test: Medium tier, only hard required |
| T102 | 1 | Three-tier turn routing (round, clamp, effective tier) | T101 | opus | done | agent/jev.go: Tier, round+clamp, TurnTier, JevRoute; tests |
| T201 | 2 | Remove jev question answering | T000 | opus | done | question.go: jev answering removed; tests; tui TestJevStats adapted (v4 test relied on removed behaviour) |
| T202 | 2 | Remove jev risk screen, approval.go back to pre-v4 | T000 | opus | todo | |
| T301 | 3 | Routing-only jev stats and /jev stats lines | T102, T201, T202 | sonnet | todo | |
| T302 | 3 | /jev prints the active route | T102 | sonnet | todo | |
| T401 | 4 | Subagent iteration limit 500, main 50 | T000 | opus | todo | |
| T501 | 5 | Click-to-cursor really moves the textarea cursor | T000 | sonnet | todo | |
| T601 | 6 | Docs: README Usage jev example, usage.md, /help, CLAUDE.md | T102, T201, T202, T301, T302, T401, T501 | sonnet | todo | |
| T799 | 6 | Global Definition of Done | all | supervisor | todo | |

Status values: `todo` · `done` · `blocked` · `superseded`. No pipe characters inside cells.

## Execution Protocol

Run with the `loop-coding` skill (`/loop-coding v5`). The authoritative procedure
(round limit, commit format, Definition-of-Done repair) is in
`.claude/skills/loop-coding/SKILL.md`; in short, per iteration:
1. Select the first `todo` row whose dependencies are all `done`.
2. Read `versions/v5/tasks/<ID>.md`; dispatch one fresh worker with the row's `Model` and a brief made of the
   Standing constraints, the task's Brief and the path of its verification script.
3. The supervisor runs `scripts/verify/v5/<ID>.sh` itself (incl. its negative checks); never trust
   the worker's claim.
4. Pass → one commit with the work and this file's update. Fail → feedback; still failing
   after 3 rounds → `blocked`.
5. Nothing selectable → run the Global Definition of Done of `versions/v5/plan.md`; done when it passes.
