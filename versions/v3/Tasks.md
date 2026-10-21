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
| T000 | 0 | Preflight | — | supervisor | done | verify passes (7 checks) |
| T101 | 1 | Session Store (Save/Load/List/Delete) | T000 | opus | done | internal/session/store.go+session.go+store_test.go; PathGuard confinement, id=SHA-256[0:3], 8 brief tests + TestTitle; verify 10/10 |
| T102 | 1 | Agent export accessors + restore setters | T101 | opus | done | HistoryEntry/ToolCall export, TurnState (A5), Restore* setters; export is redacted b/c tools registry redacts at boundary (superseded part of A13 intent); 3 rounds, 2 iteration-limit restarts |
| T103 | 1 | Save on exit (ExitSave wiring) | T101, T102 | sonnet | done | tui commands.go + keycode path, ExitSave tests; row fixed: view-secret negative scoped to exit_save_test.go (old grep matched earlier tests' legit negatives) ; files: tui/save.go+exit_save_test.go, commands.go, model.go; session file holds redacted text (agent redacts at boundary), agent/loop.go fix: finished turns were marked interrupted |
| T301 | 3 | Providertest pre-first-byte failure hooks | T000 | sonnet | done | providertest scripting: reset, 429, 5xx; row fixed: tests live in internal/provider (where verify runs them) ; files: providertest/failures.go (FailMode, NewWithFailures), providertest.go (Reset, RetryAfter), provider/fake_failures_test.go |
| T302 | 3 | Retry loop in client.go | T301 | opus | done | waits 1,2,4,8,16,32,60,60,60s, 10 attempts; negatives 400 + post-first-byte + foreign host ; client.go run/attempt loop, EventRetry, retry_test.go, export_test.go; row fixed: grep -F patterns had stray backslashes; 5xx tests adapted (500 now retries; agent tests use 400) |
| T201 | 2 | --resume flag in cmd/yip | T000 | opus | done | -c alias, unknown id exit 1, picker vs fresh decision (A6, A7) ; row fixed: hand-over check now requires Options.ResumeData/ResumePick/StdinTTY (was vacuous) ; main.go parseArgs/runWith/applyResume, Options.ResumeData/ResumePick/StdinTTY, 6 tests; combined short flags unsupported (parser has none) |
| T202 | 2 | Resume picker overlay | T201, T101, T102 | sonnet | done | picker.go pattern, newest-first rows, Enter restores, Esc exits ; tui/resume.go (resumePicker, restore), model.go wiring, resume_test.go; stored mode != auto restores as manual, empty effort keeps current |
| T203 | 2 | /sessions command | T101, T102, T103 | sonnet | done | list 10 newest, load into tab, rm y/n, R35a colors ; tui/sessions.go (ls/load/rm via Store), commands.go, help.go, model.go rmPending; restore now restore(tab, data); help_test count adapted |
| T204 | 2 | e2e Session round-trip fidelity | T101, T103, T201 | sonnet | done | e2e/session_test.go: request diff, counters, SECRET123 ; row fixed: file stores redacted text (no SECRET123), verify grep path internal/session ; e2e/session_test.go (4 tests); restart via Options.ResumeData seam (e2e cannot reach cmd/yip) |
| T401 | 4 | Tab state refactor | T103, T102 | opus | done | tabs.go per-tab struct, global model/effort/mode, /new opens tab (A6, A8) ; tabs.go + tabs_test.go; Model embeds current tab (use tabAt(i) for tab bar); Options.NewSession factory; events routed by session; 5 older tests adapted to /new-opens-tab; flaky TestSessionsLoadUnknownId fixed |
| T402 | 4 | Tab bar and keys | T401, T103 | sonnet | done | bar, Ctrl+T/W/Tab/Shift+Tab, Alt+1..8, limit line, old keys unchanged ; tabbar.go, tabkeys.go, tabbar_test.go, model.go/mouse.go; NOTE ctrl+tab / ctrl+shift+tab are not producible by bubbletea 1.3 parsing on real terminals (tests use synthetic KeyRunes); Alt+1..8 and Ctrl+T/W work |
| T501 | 5 | Question gate in agent | T000 | opus | done | question tool auto-approved, reply channel, never gated (A1, A2) ; agent/question.go (Cancelled, QuestionRequest, JoinAnswers), EvQuestion + Event.Question, offered to main agent only, loop.go cancel branch; worker's "interrupted-flag bug" note is stale (fixed in T103) |
| T502 | 5 | Question overlay in TUI | T501 | sonnet | done | radio/checkbox/free text, Esc cancelled, approval-prompt style ; tui/question.go + question_test.go, per-tab question queue, shared overlayCentered; row fixed: redactor check was inverted (expect_fail on m.redact), now a positive count check |
| T503 | 5 | e2e Question | T501, T502 | sonnet | done | answer is the tool result in the follow-up request ; e2e/question_test.go (4 tests, auto+manual mode) |
| T601 | 5 | Settings write in config | T000 | opus | done | three keys atomic, key-preserving, {env:NAME} literal, SECRET123 negative ; config/settings.go + settings_test.go; model key is default_model (what Load reads), effort/mode keys are new and Load does not read them yet (only in Config.Raw); file re-indented on write |
| T602 | 6 | Settings persist trigger in TUI | T601, T202, T402 | opus | done | model/effort/mode changes write settings, failure line (A11) ; tui/settings.go + settings_test.go, Options.SettingsPath/SaveSettings seam, main.go sets GlobalPath; synchronous save after change; real ~/.config untouched by tests (hash checked) |
| T603 | 6 | Shift+enter newline and help lines | T000 | haiku | done | shift+enter key msg inserts newline, alt+enter unchanged, /help lines ; model.go binding, help.go row, newline_test.go, help_test.go, main_test.go; bubbletea 1.3.10 has no shift+enter key type so real terminals rarely deliver it (tests use a synthetic named key) |
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
