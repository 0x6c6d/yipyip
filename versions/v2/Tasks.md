# Tasks v2: yipyip, a UI that feels like Claude Code

_Generated from versions/v2/plan.md. This file is the loop's persistent state (status and notes
only): update it in place, never regenerate it during a build. Briefs live in `versions/v2/tasks/<ID>.md`,
verifications in `scripts/verify/v2/<ID>.sh`. IDs are unique within this version._

## Standing constraints

The supervisor prepends these, verbatim, to every worker brief:

- `make check` must pass for the whole repo (`gofmt -l .` prints nothing, `go vet ./...`, `go test ./...`, `go build ./...`). Go 1.27, module `yipyip`. Adapt existing tests only where your task changes the behaviour they assert; never delete a test to make the build green.
- Protected (never modify): `README.md`, `Plan.md`, `prompts/`, `.claude/`, `versions/` (all of it), `scripts/verify/`, and the "Delegation workflow" section of `CLAUDE.md`. v2 is TUI-only: never modify `internal/agent`, `internal/tools`, `internal/provider` or `internal/config` (checked with `git diff d3eeba4`).
- No commits, no `git init`, no branch changes. No network: v2 needs no new module download (`github.com/atotto/clipboard v0.1.4` and `github.com/aymanbagabas/go-osc52/v2 v2.0.1` are already in `go.sum`); if the go tool wants to fetch, run it with `GOPROXY=off` and fix the cause. Only `proxy.golang.org` may ever be contacted.
- Tests run offline against the `httptest` fake provider in `internal/provider/providertest`; no test touches the real clipboard, runs `wl-copy`/`xclip`/`xsel`, contacts a host, or waits for UI time with `time.Sleep` (use the injected clock `Options.Now` once it exists). No teatest: TUI tests drive `Model.Update` / `Model.Command` / `Model.Drain` and assert on `View()` (strip ANSI with `github.com/charmbracelet/x/ansi`'s `ansi.Strip` when checking plain text).
- Tests that check colours force a lipgloss colour profile with the helper `withColorProfile(t, termenv.ANSI256)` or `withColorProfile(t, termenv.TrueColor)` from `internal/tui/color_test.go` and never call `t.Parallel()`. If that file does not exist yet, create it with exactly this helper (package `tui`; imports `testing`, `github.com/charmbracelet/lipgloss`, `github.com/muesli/termenv`): `func withColorProfile(t *testing.T, p termenv.Profile) { t.Helper(); old := lipgloss.ColorProfile(); lipgloss.SetColorProfile(p); t.Cleanup(func() { lipgloss.SetColorProfile(old) }) }`.
- yipyip writes no files of its own: input history, paste buffers and the message queue live in memory only. All text shown or copied passes the session redactor (`Session.Redact`), so a secret such as `SECRET123` never reaches the screen or the clipboard.
- Name new tests exactly as the brief says (the verification runs them by name and fails when one is missing).

## Task List

| ID | Phase | Title | Depends-on | Model | Status | Notes |
|----|-------|-------|------------|-------|--------|-------|
| T000 | 0 | Preflight | — | supervisor | todo | |
| T101 | 1 | Two-row status area | T000 | sonnet | todo | |
| T102 | 1 | Mode label colours and key hint | T101 | opus | todo | |
| T103 | 1 | Auto start mode in cmd/yip | T102 | opus | todo | |
| T201 | 2 | Borderless tool-call blocks | T000 | sonnet | todo | |
| T202 | 2 | Borderless message blocks | T201 | sonnet | todo | |
| T203 | 2 | Turn clock and Thinking line | T000 | sonnet | todo | |
| T204 | 2 | Finished marker | T203 | sonnet | todo | |
| T301 | 3 | Live subagent blocks | T201, T203 | sonnet | todo | |
| T302 | 3 | Subagent panel format | T301 | haiku | todo | |
| T401 | 4 | Command autosuggest ghost text | T000 | sonnet | todo | |
| T402 | 4 | Input history | T000 | opus | todo | |
| T403 | 4 | Collapsed paste placeholders | T402 | opus | todo | |
| T501 | 5 | Queue messages while busy | T204, T402, T403 | opus | todo | |
| T502 | 5 | Restore queued messages into the input | T501 | opus | todo | |
| T601 | 6 | Mouse wheel and key scrolling | T000 | sonnet | todo | |
| T602 | 6 | Clipboard writer | T000 | opus | todo | |
| T603 | 6 | Copy-on-select | T204, T601, T602 | opus | todo | |
| T701 | 7 | Help lists the new keys | T000 | haiku | todo | |
| T702 | 7 | Usage docs and CLAUDE.md notes | T103, T204, T302, T403, T502, T603, T701 | haiku | todo | |
| T799 | 7 | Global Definition of Done | T101, T102, T103, T201, T202, T203, T204, T301, T302, T401, T402, T403, T501, T502, T601, T602, T603, T701, T702 | supervisor | todo | |

Status values: `todo` · `done` · `blocked` · `superseded`. No pipe characters inside cells.

## Execution Protocol

Run with the `loop-coding` skill (`/loop-coding v2`). The authoritative procedure
(round limit, commit format, Definition-of-Done repair) is in
`.claude/skills/loop-coding/SKILL.md`; in short, per iteration:
1. Select the first `todo` row whose dependencies are all `done`.
2. Read `versions/v2/tasks/<ID>.md`; dispatch one fresh worker with the row's `Model` and a brief made of the
   Standing constraints, the task's Brief and the path of its verification script.
3. The supervisor runs `scripts/verify/v2/<ID>.sh` itself (incl. its negative checks); never trust
   the worker's claim.
4. Pass → one commit with the work and this file's update. Fail → feedback; still failing
   after 3 rounds → `blocked`.
5. Nothing selectable → run the Global Definition of Done of `versions/v2/plan.md`; done when it passes.
