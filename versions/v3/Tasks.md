# Tasks v3: sessions, tabs, questions — a durable daily driver

_Generated from versions/v3/plan.md. This file is the loop's persistent state (status and notes
only): update it in place, never regenerate it during a build. Briefs live in `versions/v3/tasks/<ID>.md`,
verifications in `scripts/verify/v3/<ID>.sh`. IDs are unique within this version._

## Standing constraints

The supervisor prepends these, verbatim, to every worker brief:

- `make check` must pass for the whole repo (`gofmt -l .` prints nothing, `go vet ./...`, `go test ./...`, `go build ./...`). Go 1.27, module `yipyip`. Adapt existing tests only where your task changes the behaviour they assert; never delete a test to make the build green.
- Protected (never modify, checked as `git diff --stat 9519824..HEAD -- <path>` plus `git status --porcelain -- <path>`, both empty): `internal/provider/transport.go`, `internal/tools/` (all of it), `README.md`, `Plan.md`, `prompts/`, `.claude/`, `versions/` (all of it), `scripts/verify/`, and the "Delegation workflow" section of `CLAUDE.md`. Nothing else stays frozen: `internal/agent`, `internal/provider/client.go` and `internal/provider/providertest` are all modified in v3.
- No commits, no `git init`, no branch changes. No network: v3 needs no new modules (`go.mod` never changes); if the go tool wants to fetch, run it with `GOPROXY=off` and fix the cause. Only `proxy.golang.org` may ever be contacted, never at test time.
- Tests run offline against the `httptest` fake provider in `internal/provider/providertest` and in temp dirs / temp `HOME`; no test contacts a host, waits for real UI time, or depends on wall-clock timing for logic that can use an injected clock/value. No teatest: TUI tests drive `Model.Update` / `Model.Command` and assert on `View()` (strip ANSI with `github.com/charmbracelet/x/ansi`'s `ansi.Strip` when checking plain text).
- Tests that check colours force a lipgloss colour profile with the helper `withColorProfile(t, termenv.ANSI256)` or `withColorProfile(t, termenv.TrueColor)` from `internal/tui/color_test.go` and never call `t.Parallel()`.
- Secrets: a resolved `{env:NAME}` value such as `SECRET123` never reaches any file yipyip writes (session files, settings), the redacted view, or a test assertion where it would be expected; use the redactor, and include the `SECRET123` negative the brief asks for.
- Public interface is frozen once written in this version: session file `version` = 1, `<id>` = 12 hex chars (first 3 bytes of SHA-256 of the file content), tool names, slash commands. Renaming later is breaking.
- Name new tests exactly as the brief says (the verification runs them by name and fails when one is missing).

## Task List

| ID | Phase | Title | Depends-on | Model | Status | Notes |
|----|-------|-------|------------|-------|--------|-------|
| T000 | 0 | Preflight | — | supervisor | todo | |
| T101 | 1 | Session Store (Save/Load/List/Delete) | T000 | opus | todo | internal/session, atomic, symlink + SECRET123 negatives, .gitignore |
| T102 | 1 | Agent export accessors + restore setters | T101 | opus | todo | agent/session.go only; interrupted-as-finished |
| T103 | 1 | Save on exit (ExitSave wiring) | T101, T102 | sonnet | todo | tui commands.go + keycode path, ExitSave tests |
| T301 | 3 | Providertest pre-first-byte failure hooks | T000 | sonnet | todo | providertest scripting: reset, 429, 5xx |
| T302 | 3 | Retry loop in client.go | T301 | opus | todo | waits 1,2,4,8,16,32,60,60,60s, 10 attempts; negatives 400 + post-first-byte + foreign host |
| T201 | 2 | --resume flag in cmd/yip | T000 | opus | todo | -c alias, unknown id exit 1, picker vs fresh decision (A6, A7) |
| T202 | 2 | Resume picker overlay | T201, T101, T102 | sonnet | todo | picker.go pattern, newest-first rows, Enter restores, Esc exits |
| T203 | 2 | /sessions command | T101, T102, T103 | sonnet | todo | list 10 newest, load into tab, rm y/n, R35a colors |
| T204 | 2 | e2e Session round-trip fidelity | T101, T103, T201 | sonnet | todo | e2e/session_test.go: request diff, counters, SECRET123 |
| T401 | 4 | Tab state refactor | T103, T102 | opus | todo | tabs.go per-tab struct, global model/effort/mode, /new opens tab (A6, A8) |
| T402 | 4 | Tab bar and keys | T401, T103 | sonnet | todo | bar, Ctrl+T/W/Tab/Shift+Tab, Alt+1..8, limit line, old keys unchanged |
| T501 | 5 | Question gate in agent | T000 | opus | todo | question tool auto-approved, reply channel, never gated (A1, A2) |
| T502 | 5 | Question overlay in TUI | T501 | sonnet | todo | radio/checkbox/free text, Esc cancelled, approval-prompt style |
| T503 | 5 | e2e Question | T501, T502 | sonnet | todo | answer is the tool result in the follow-up request |
| T601 | 5 | Settings write in config | T000 | opus | todo | three keys atomic, key-preserving, {env:NAME} literal, SECRET123 negative |
| T602 | 6 | Settings persist trigger in TUI | T601, T202, T402 | opus | todo | model/effort/mode changes write settings, failure line (A11) |
| T603 | 6 | Shift+enter newline and help lines | T000 | haiku | todo | shift+enter key msg inserts newline, alt+enter unchanged, /help lines |
| T604 | 6 | Usage docs, CLAUDE.md and --help | T201, T203, T302, T402, T502, T602, T603 | haiku | todo | docs/usage.md Sessions/Tabs/Questions/Retry, CLAUDE.md outside Delegation section |
| T799 | 9 | Global Definition of Done | T101, T102, T103, T201, T202, T203, T204, T401, T402, T501, T502, T503, T601, T602, T603, T604 | supervisor | todo | runs the plan's DoD lines plus make check |

Status values: `todo` · `done` · `blocked` · `superseded`. No pipe characters inside cells.

## Changelog
- batch 2: phases 2, 4, 5 and 6 decomposed (T201–T204, T401–T402, T501–T503, T601–T604) plus
  supervisor row T799; appended after batch 1.

## Execution Protocol

Run with the `loop-coding` skill (`/loop-coding v3`). The authoritative procedure
(round limit, commit format, Definition-of-Done repair) is in
`.claude/skills/loop-coding/SKILL.md`; in short, per iteration:
1. Select the first `todo` row whose dependencies are all `done`.
2. Read `versions/v3/tasks/<ID>.md`; dispatch one fresh worker with the row's `Model` and a brief made of the
   Standing constraints, the task's Brief and the path of its verification script.
3. The supervisor runs `scripts/verify/v3/<ID>.sh` itself (incl. its negative checks); never trust
   the worker's claim.
4. Pass → one commit with the work and this file's update. Fail → feedback; still failing
   after 3 rounds → `blocked`.
5. Nothing selectable → run the Global Definition of Done of `versions/v3/plan.md`; run
   `versions/v3/tasks/T799.md` (supervisor row) when it becomes selectable.
