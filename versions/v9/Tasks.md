# Tasks v9: yipyip (/clear in place, last tab replaced, rendered markdown)

_Generated from versions/v9/plan.md. This file is the loop's persistent state (status and notes
only): update it in place, never regenerate it during a build. Briefs live in `versions/v9/tasks/<ID>.md`,
verifications in `scripts/verify/v9/<ID>.sh`. IDs are unique within this version._

## Standing constraints

The supervisor prepends these, verbatim, to every worker brief:

- `make check` must pass for the whole repo (`gofmt -l .` prints nothing, `go vet ./...`, `go test ./...`, `go build ./...`). Go 1.27, module `yipyip`, repo root `/home/sa/workspace/code/yipyip`.
- Protected (never modify): `Plan.md`, `versions/`, `scripts/verify/`, `prompts/`, `.claude/`; `README.md` sections 1-9 (only the `## Usage` section at the end, and only when the brief says so); the "Delegation workflow" section of `CLAUDE.md`; `go.mod`, `go.sum`; the five security files `internal/provider/transport.go`, `internal/tools/pathguard.go`, `internal/tools/bash.go`, `internal/agent/approval.go`, `internal/config/redact.go` (all stay identical to commit `a4e4cbb`).
- Never create, edit or delete files named `zz_verify_*` (the supervisor's temporary probe tests).
- No commits, no `git init`/`stash`/`reset`/`checkout`; the supervisor commits. No network except `proxy.golang.org` (and none is needed: no new modules, standard library plus the modules already in `go.mod`). Tests run offline against `internal/provider/providertest`; no real host in any test.
- Change only the files the brief names plus their `_test.go` files. An existing test not named in the brief may be changed only if it breaks because of the behaviour the brief asks for; say which one and why in your report.
- These `internal/tui` test helpers keep their names and signatures (supervisor probes call them): `newSessionModel`, `newSessionModelOpts`, `answerServer`, `tabTurn`, `mustAddTab`, `feed`, `newBlockModel`, `plainView`, `belowTabBar`, `waitSession`, `waitBusy`, `sessionFiles`, `readSavedFile`, `countInfo`, `clickBar`, `zoneCol`, `withColorProfile`, `histKey`.
- TUI tests drive `Model.Update` and `View()` (no teatest); compare rendered text with `ansi.Strip` (`github.com/charmbracelet/x/ansi`). A resolved secret (`SECRET123` in tests) never appears in the view, the clipboard or any written file.
- New functions get a doc comment; keep the existing code style.

## Task List

| ID | Phase | Title | Depends-on | Model | Status | Notes |
|----|-------|-------|------------|-------|--------|-------|
| T000 | 0 | Preflight | — | supervisor | done | preflight passes |
| T101 | 1 | Markdown renderer skeleton: block pass, fences, headings, bold | T000 | opus | done | markdown.go, markdown_test.go, highlight.go fence helpers |
| T102 | 1 | Assistant blocks through renderMarkdown with the secret guard | T101 | opus | done | blocks.go wiring, secret guard, tests |
| T201 | 2 | Tables at natural width | T101 | opus | done | mdTableRule, tests |
| T202 | 2 | Tables wrapped to the width | T201 | opus | done | table wrapping in markdown.go, tests |
| T203 | 2 | Bullet and numbered lists | T101 | opus | done | mdListRule, tests; fence ends a list |
| T204 | 2 | Blockquotes and horizontal rules | T101 | opus | done | quote and rule rules, tests |
| T205 | 2 | Inline italic, strike, code, links, escapes | T102 | opus | done | inline pass: emphasis, code, links, escapes |
| T206 | 2 | Incomplete input, fence isolation, robustness, fuzz seeds | T102 T202 T203 T204 T205 | opus | done | linear inline index, depth cap, fuzz seeds |
| T301 | 3 | resetTab helper | T000 | opus | done | resetTab in tabs.go, reset_test.go |
| T302 | 3 | /clear in place and its /help entry | T301 | opus | todo | |
| T401 | 4 | Closing the last tab leaves one fresh tab | T301 | opus | todo | |
| T501 | 5 | e2e Markdown tests: table view and copy | T102 T202 | sonnet | todo | |
| T502 | 5 | docs/usage.md and README Usage line | T206 T302 T401 | haiku | todo | |
| T503 | 5 | CLAUDE.md tui and e2e notes | T206 T302 T401 | haiku | todo | |
| T599 | 5 | Global Definition of Done | T501 T502 T503 | supervisor | todo | |

Status values: `todo` · `done` · `blocked` · `superseded`. No pipe characters inside cells.

## Execution Protocol

Run with the `loop-coding` skill (`/loop-coding v9`). The authoritative procedure
(round limit, commit format, Definition-of-Done repair) is in
`.claude/skills/loop-coding/SKILL.md`; in short, per iteration:
1. Select the first `todo` row whose dependencies are all `done`.
2. Read `versions/v9/tasks/<ID>.md`; dispatch one fresh worker with the row's `Model` and a brief made of the
   Standing constraints, the task's Brief and the path of its verification script.
3. The supervisor runs `scripts/verify/v9/<ID>.sh` itself (incl. its negative checks); never trust
   the worker's claim. Scripts with probes copy `zz_verify_probe_test.go` into a package for one
   run and remove it; `git status` must show no such file before a commit.
4. Pass → one commit with the work and this file's update. Fail → feedback; still failing
   after 3 rounds → `blocked`.
5. Nothing selectable → run the Global Definition of Done of `versions/v9/plan.md` (`T599`); done when it passes.
