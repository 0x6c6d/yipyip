# Tasks v7: yipyip (no model-call limit for the main agent)

_Generated from versions/v7/plan.md. This file is the loop's persistent state (status and notes
only): update it in place, never regenerate it during a build. Briefs live in `versions/v7/tasks/<ID>.md`,
verifications in `scripts/verify/v7/<ID>.sh`. IDs are unique within this version._

## Standing constraints

The supervisor prepends these, verbatim, to every worker brief:

- `make check` must pass for the whole repo (`gofmt -l .` empty, `go vet ./...`, `go test ./...`, `go build ./...`).
- Protected (never modify): `Plan.md`, `README.md`, `versions/`, `scripts/verify/`, `prompts/`, `.claude/`, the "Delegation workflow" section of `CLAUDE.md`, `go.mod`, `go.sum`, `internal/agent/approval.go`.
- Already done and verify-only: `docs/usage.md`, `CLAUDE.md`, the main-agent change in `internal/agent/loop.go`. `const MaxIterations = 0` (main agent unlimited) and `const MaxSubagentIterations = 500` stay exactly as they are; never write the text `iteration limit (50)` anywhere under `internal/agent/`.
- Do not commit, do not run `git init`, `git stash`, `git checkout` or `git reset`. No network access except `proxy.golang.org` via `go mod download` (no new modules are needed). Tests are offline: use the fake provider in `internal/provider/providertest`, never a real host.
- Run the verification script named in your brief yourself before reporting; never edit it.

## Task List

| ID | Phase | Title | Depends-on | Model | Status | Notes |
|----|-------|-------|------------|-------|--------|-------|
| T000 | 0 | Preflight | — | supervisor | done | preflight script passes |
| T101 | 1 | TestMainCancelLongTurn (cancel in the 60th tool round) | T000 | opus | done | loop_test.go: TestMainCancelLongTurn (60th round, 2 s bound, history intact) |
| T102 | 1 | Verify already-done R64/R66 files and v1 T403 | T000 | supervisor | todo | |
| T201 | 2 | README R6 requirement and acceptance line | T000 | supervisor | todo | |
| T202 | 2 | Rewrite scripts/verify/v5/T401.sh for R64 | T000 | supervisor | todo | |
| T203 | 2 | Rewrite scripts/verify/v5/T799.sh for R64, rebase drift checks | T202 | supervisor | todo | |
| T204 | 2 | Commit and Phase 2 exit (v5 scripts pass on the committed tree) | T101, T201, T202, T203 | supervisor | todo | |
| T299 | 2 | Global Definition of Done | T102, T204 | supervisor | todo | |

Status values: `todo` · `done` · `blocked` · `superseded`. No pipe characters inside cells.

## Execution Protocol

Run with the `loop-coding` skill (`/loop-coding v7`). The authoritative procedure
(round limit, commit format, Definition-of-Done repair) is in
`.claude/skills/loop-coding/SKILL.md`; in short, per iteration:
1. Select the first `todo` row whose dependencies are all `done`.
2. Read `versions/v7/tasks/<ID>.md`; dispatch one fresh worker with the row's `Model` and a brief made of the
   Standing constraints, the task's Brief and the path of its verification script.
   Rows with Model `supervisor` get no worker: the supervisor makes the edit described in the task file itself.
3. The supervisor runs `scripts/verify/v7/<ID>.sh` itself (incl. its negative checks); never trust
   the worker's claim.
4. Pass → one commit with the work and this file's update. Fail → feedback; still failing
   after 3 rounds → `blocked`.
5. Nothing selectable → run the Global Definition of Done of `versions/v7/plan.md`; done when it passes.
