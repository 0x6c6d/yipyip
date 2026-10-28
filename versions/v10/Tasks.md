# Tasks v10: yipyip (crash log, checkpoints, autosave, retention)

_Generated from versions/v10/plan.md. This file is the loop's persistent state (status and notes
only): update it in place, never regenerate it during a build. Briefs live in `versions/v10/tasks/<ID>.md`,
verifications in `scripts/verify/v10/<ID>.sh`. IDs are unique within this version._

## Standing constraints

The supervisor prepends these, verbatim, to every worker brief:

- `make check` must pass for the whole repo (`gofmt -l .` prints nothing, `go vet ./...`, `go test ./...`, `go build ./...`). Go 1.27, module `yipyip`, repo root `/home/sa/workspace/code/yipyip`.
- Protected (never modify): `Plan.md`, `versions/`, `scripts/verify/`, `prompts/`, `.claude/`; `README.md` sections 1-9 (only the `## Usage` section at the end, and only when the brief says so); the "Delegation workflow" section of `CLAUDE.md`; `go.mod`, `go.sum`; the five security files `internal/provider/transport.go`, `internal/tools/pathguard.go`, `internal/tools/bash.go`, `internal/agent/approval.go`, `internal/config/redact.go` (all stay identical to commit `1ccbbc7`).
- Never create, edit or delete files named `zz_verify_*` (the supervisor's temporary probe tests).
- No commits, no `git init`/`stash`/`reset`/`checkout`; the supervisor commits. No network except `proxy.golang.org` (and none is needed: no new modules, standard library plus the modules already in `go.mod`). Tests run offline against `internal/provider/providertest`; no real host in any test.
- Change only the files the brief names plus their `_test.go` files. An existing test not named in the brief may be changed only if it breaks because of the behaviour the brief asks for; say which one and why in your report.
- The `internal/tui` and `e2e` test helpers keep their names and signatures (supervisor probes and later tasks call them): `newSessionModel`, `newSessionModelOpts`, `answerServer`, `tabTurn`, `mustAddTab`, `feed`, `newBlockModel`, `plainView`, `belowTabBar`, `waitSession`, `waitBusy`, `sessionFiles`, `readSavedFile`, `countInfo`, `clickBar`, `zoneCol`, `withColorProfile`, `histKey`, and in `e2e`: `newEnv`, `newEnvServer`, `envOpts`, `waitSession`, `(*env).submit`, `(*env).path`, `(*env).write`, `(*env).read`.
- A resolved secret (`SECRET123` in tests) never appears in `crash.log`, the view, the clipboard or any written file. `crash.log` holds only version, time, where, session id, the redacted panic value and the redacted stack: never history, prompts, tool arguments or tool output.
- TUI tests drive `Model.Update` and `View()` (no teatest); compare rendered text with `ansi.Strip` (`github.com/charmbracelet/x/ansi`). Tests that touch the crash log use `t.TempDir()` and restore the package default with `crashlog.SetDefault(nil)` in `t.Cleanup`.
- New functions get a doc comment; keep the existing code style.

## Task List

| ID | Phase | Title | Depends-on | Model | Status | Notes |
|----|-------|-------|------------|-------|--------|-------|
| T000 | 0 | Preflight | — | supervisor | done | preflight passed |
| T101 | 1 | internal/crashlog: records, redaction, cap, perms | T000 | opus | done | internal/crashlog (crashlog.go + test); opus first round |
| T102 | 1 | Fatal panic path: cmd/yip exit 2, tui panic guard | T101 | opus | done | cmd/yip crash.go+main.go, tui/crashguard.go; opus first round |
| T201 | 2 | Log recovered panics (tool, turn, subagent, stream) | T101 | opus | done | Recovered() at tool/turn/subagent/stream sites; opus first round |
| T202 | 2 | Main-agent history checkpoints and EvCheckpoint | T000 | opus | done | checkpoint() + EvCheckpoint + roundHook in agent; opus first round |
| T301 | 3 | Store.Prune | T000 | opus | done | Store.Prune + prune_test.go; opus first round; ignores symlinked session files (stricter than List) |
| T302 | 3 | TUI autosave, exit-save coordination, prune after save | T202 T301 | opus | done | tui autosave.go, save.go, model.go; opus first round; 10 existing tests got noAutosave, e2e secret_test allows session files; NOTE pre-existing agent bug: runTurn cancel() before deferred ctx.Err() marks every turn interrupted (out of v10 scope) |
| T303 | 3 | Startup prune protecting the resumed id | T301 | opus | done | pruneSessions in cmd/yip/main.go + prune_test.go; opus first round |
| T401 | 4 | Subagent panic leaves the parent turn alive | T201 | sonnet | done | subHook seam in agenttools.go + 3 tests; sonnet first round |
| T402 | 4 | Per-block render guard | T201 | sonnet | done | renderBlock recover guard in blocks.go + renderguard_test.go; sonnet first round |
| T501 | 5 | e2e: Crash, Autosave, Retention | T102 T201 T202 T302 T303 | sonnet | done | e2e crash/autosave/retention tests, helpers additive (tuiOpts); sonnet first round |
| T502 | 5 | docs/usage.md and README Usage line | T402 T501 | haiku | done | docs/usage.md section + Security bullet fix + README Usage line; haiku, 1 feedback round |
| T503 | 5 | CLAUDE.md notes | T402 T501 | haiku | todo | |
| T599 | 5 | Global Definition of Done | T401 T402 T501 T502 T503 | supervisor | todo | |

Status values: `todo` · `done` · `blocked` · `superseded`. No pipe characters inside cells.

## Execution Protocol

Run with the `loop-coding` skill (`/loop-coding v10`). The authoritative procedure
(round limit, commit format, Definition-of-Done repair) is in
`.claude/skills/loop-coding/SKILL.md`; in short, per iteration:
1. Select the first `todo` row whose dependencies are all `done`.
2. Read `versions/v10/tasks/<ID>.md`; dispatch one fresh worker with the row's `Model` and a brief made of the
   Standing constraints, the task's Brief and the path of its verification script.
3. The supervisor runs `scripts/verify/v10/<ID>.sh` itself (incl. its negative checks); never trust
   the worker's claim.
4. Pass → one commit with the work and this file's update. Fail → feedback; still failing
   after 3 rounds → `blocked`.
5. Nothing selectable → run the Global Definition of Done of `versions/v10/plan.md`; done when it passes.
