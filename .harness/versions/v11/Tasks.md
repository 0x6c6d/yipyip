# Tasks v11: yipyip (headless mode + paste display fix)

_Generated from .harness/versions/v11/plan.md. This file is the loop's persistent state (status and notes
only): update it in place, never regenerate it during a build. Briefs live in `.harness/versions/v11/tasks/<ID>.md`,
verifications in `.harness/verify/v11/<ID>.sh`. IDs are unique within this version._

## Standing constraints

The supervisor prepends these, verbatim, to every worker brief:

- `make check` must pass for the whole repo (`gofmt -l .` empty, `go vet ./...`, `go test ./...`, `go build ./...`).
- Repo root: `/home/sa/workspace/code/yipyip`, Go module `yipyip`, Go 1.27, stdlib only: no new modules, no edits to `go.mod`/`go.sum`.
- Protected (never modify): the whole `.harness/` folder (spec, plans, tasks, verify scripts), `.claude/`, and the "Delegation workflow" section of `CLAUDE.md`. Change nothing outside the files your task names (plus their `_test.go` files); `README.md` and `CLAUDE.md` only in the task that names them.
- Never commit, never run `git init`, `git reset`, `git checkout` or `git stash`; the supervisor commits.
- No network except `proxy.golang.org` through `go mod download`; tests run offline against `internal/provider/providertest` (fake provider) in temp dirs with a temp `HOME`; never a real host in tests or the Makefile.
- Security-critical logic stays in `internal/provider/transport.go`, `internal/tools/pathguard.go`, `internal/tools/bash.go`, `internal/agent/approval.go`, `internal/config/redact.go`, `internal/crashlog/`: call them, never re-implement them. Non-test code never uses `http.DefaultClient`, `http.DefaultTransport` or `http.Get`/`Post`/`Head`.
- Every string that reaches stdout, stderr or a file written by the headless runner passes the session redactor (`(*agent.Session).Redact`); a config secret (`{env:NAME}` value) never appears in any output.
- Existing tests keep passing unchanged unless the brief says otherwise; the headless runner never writes `config.json` (no `config.SaveSettings` call) and never starts a Bubble Tea program.

## Task List

| ID | Phase | Title | Depends-on | Model | Status | Notes |
|----|-------|-------|------------|-------|--------|-------|
| T000 | 0 | Preflight | — | supervisor | done | preflight passed |
| T101 | 1 | Headless flag parsing | T000 | sonnet | done | cmd/yip headless flag parsing, stub runHeadless |
| T102 | 1 | Headless runner skeleton and e2e harness | T101 | opus | done | headless.go runner, e2e/headless_test.go |
| T201 | 2 | Agent SystemAppend option | T000 | sonnet | done | Options.SystemAppend in session.go + test |
| T202 | 2 | Shared MaxModelCalls counter | T000 | opus | done | atomic shared counter in session.go+loop.go; a counted request cut off by cancel still uses its slot (never exceeds n) |
| T203 | 2 | Session FromAgent and ApplyTo helpers | T000 | opus | done | session/agent.go FromAgent/ApplyTo, tui uses them |
| T301 | 3 | JSONL record mapping | T101 | sonnet | done | headless_events.go; approvalRecord and resultRecord fields NOT redacted by mapper: T302/T303 must redact them |
| T302 | 3 | JSON output and stderr progress wiring | T102, T202, T301 | opus | done | runner struct in headless.go, e2e/headless_json_test.go; approval/result fields redacted in runner |
| T303 | 3 | Transcript file and secret redaction | T302 | opus | done | --transcript via PathGuard.Resolve, 0600, redacted |
| T401 | 4 | Slash prompts and skills | T102 | sonnet | done | skills in headless.go, tui.IsBuiltinCommand |
| T402 | 4 | Answer flag and question exit | T302 | opus | done | runner.question, exit 3 |
| T403 | 4 | Approve flag and manual mode | T302 | opus | done | --mode, --approve, Session.ToolNames |
| T404 | 4 | Timeout and max-model-calls exits | T302 | opus | done | exitFor, timeout, max_calls, signals |
| T405 | 4 | Session save, resume and no-save | T203, T302 | opus | done | runner.resume/saveSession, prune 25; model/effort flags left to T406 |
| T406 | 4 | Dir, overrides and system-append | T201, T302 | opus | todo | |
| T407 | 4 | Exit code tests 0, 1, 2 | T302 | sonnet | todo | |
| T501 | 5 | Paste text shown expanded | T000 | haiku | todo | |
| T601 | 6 | Help text for headless mode | T101 | haiku | todo | |
| T602 | 6 | README headless section | T303, T401, T402, T403, T404, T405, T406, T407, T501, T601 | sonnet | todo | |
| T603 | 6 | CLAUDE.md architecture notes | T303, T401, T402, T403, T404, T405, T406, T407, T501, T601 | haiku | todo | |
| T999 | 6 | Definition of Done | T101, T102, T201, T202, T203, T301, T302, T303, T401, T402, T403, T404, T405, T406, T407, T501, T601, T602, T603 | supervisor | todo | |

Status values: `todo` · `done` · `blocked` · `superseded`. No pipe characters inside cells.

## Execution Protocol

Run with the `loop-coding` skill (`/loop-coding v11`). The authoritative procedure
(round limit, commit format, Definition-of-Done repair) is in
`.claude/skills/loop-coding/SKILL.md`; in short, per iteration:
1. Select the first `todo` row whose dependencies are all `done`.
2. Read `.harness/versions/v11/tasks/<ID>.md`; dispatch one fresh worker with the row's `Model` and a brief made of the
   Standing constraints, the task's Brief and the path of its verification script.
3. The supervisor runs `.harness/verify/v11/<ID>.sh` itself (incl. its negative checks); never trust
   the worker's claim.
4. Pass → one commit with the work and this file's update. Fail → feedback; still failing
   after 3 rounds → `blocked`.
5. Nothing selectable → run the Global Definition of Done of `.harness/versions/v11/plan.md`; done when it passes.
