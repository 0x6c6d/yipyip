# Tasks v12: yipyip, no leftover processes after exit

_Generated from .harness/versions/v12/plan.md. This file is the loop's persistent state (status and notes
only): update it in place, never regenerate it during a build. Briefs live in `.harness/versions/v12/tasks/<ID>.md`,
verifications in `.harness/verify/v12/<ID>.sh`. IDs are unique within this version._

## Standing constraints

The supervisor prepends these, verbatim, to every worker brief:

- `make check` must pass for the whole repo (`gofmt -l .` prints nothing, `go vet ./...`, `go test ./...`, `go build ./...`).
- Protected (never modify): the whole `.harness/` folder, `.claude/`, and the "Delegation workflow" section of `CLAUDE.md`. Change nothing outside the files your task names (plus new test files of your task).
- Do not commit, do not push, do not change git config.
- Go stdlib plus the existing dependencies only: no new modules in `go.mod`/`go.sum`; no network access except `proxy.golang.org` for already listed modules; tests run offline against the fake provider in `internal/provider/providertest`.
- Linux only is required to pass (`/proc` is available); non-Linux builds must still compile where the brief says so.
- Security rule: only `internal/tools` sends signals (`syscall.Kill`) or starts processes (`os/exec`, besides the existing clipboard dependency); every other package calls `tools.KillAll`/`tools.InitSubreaper`. Non-test code never uses `http.DefaultClient`.
- Tests that start processes use unique `sleep` durations (e.g. `sleep 3011`), find them via `/proc/<pid>/cmdline` or `echo $!`, and SIGKILL whatever they started in `t.Cleanup`. A process counts as gone when `/proc/<pid>` is missing or its state is `Z`. No `t.Parallel` in tests that call `tools.KillAll`.

## Task List

| ID | Phase | Title | Depends-on | Model | Status | Notes |
|----|-------|-------|------------|-------|--------|-------|
| T000 | 0 | Preflight | — | supervisor | done | verify passed |
| T101 | 1 | Process tracker and KillAll for tracked groups | T000 | opus | done | reaper.go, reaper_linux.go, reaper_other.go, bash.go, reaper_test.go: tracker + KillAll |
| T201 | 2 | Subreaper, descendant walk, zombie reaping | T101 | opus | done | reaper*.go, reaper_test.go: subreaper, descendant walk, zombie reap |
| T301 | 3 | agent Session.Shutdown | T101 | opus | done | loop.go Session.Shutdown, shutdown_test.go |
| T302 | 3 | cmd/yip exit cleanup, headless and crash paths | T201, T301 | opus | done | main.go exitCleanup+InitSubreaper, headless.go SIGHUP+Shutdown, e2e/cleanup_test.go |
| T303 | 3 | TUI quit shutdown and SIGTERM/SIGHUP handling | T301, T302 | opus | todo | |
| T401 | 4 | System prompt exit sentence | T000 | haiku | todo | |
| T402 | 4 | /help cleanup line | T000 | haiku | todo | |
| T403 | 4 | README cleanup section | T302, T303, T401, T402 | sonnet | todo | |
| T404 | 4 | CLAUDE.md architecture notes | T201, T301, T302, T303 | haiku | todo | |
| T999 | 4 | Definition of Done | T000, T101, T201, T301, T302, T303, T401, T402, T403, T404 | supervisor | todo | |

Status values: `todo` · `done` · `blocked` · `superseded`. No pipe characters inside cells.

## Execution Protocol

Run with the `loop-coding` skill (`/loop-coding v12`). The authoritative procedure
(round limit, commit format, Definition-of-Done repair) is in
`.claude/skills/loop-coding/SKILL.md`; in short, per iteration:
1. Select the first `todo` row whose dependencies are all `done`.
2. Read `.harness/versions/v12/tasks/<ID>.md`; dispatch one fresh worker with the row's `Model` and a brief made of the
   Standing constraints, the task's Brief and the path of its verification script.
3. The supervisor runs `.harness/verify/v12/<ID>.sh` itself (incl. its negative checks); never trust
   the worker's claim.
4. Pass → one commit with the work and this file's update. Fail → feedback; still failing
   after 3 rounds → `blocked`.
5. Nothing selectable → run the Global Definition of Done of `.harness/versions/v12/plan.md`; done when it passes.
