# yipyip v2: a UI that feels like Claude Code

## 1. Goal

v2 is a pure TUI increment (`internal/tui`, `cmd/yip`): a cleaner two-row status area, command
autosuggest, input history, copy-on-select, collapsed pastes, full-conversation scrolling, and a
conversation layout modelled on Claude Code (no boxes, `●` for replies and tool calls, `⎿` for
results), a live `Thinking…` indicator with elapsed time, and Claude-Code-style subagent display. No change to the agent, tools, provider or config.

## 2. New requirements

- R19. Autosuggest: while the input is a single line, the cursor is at its end and the text starts with `/`, the rest of the first matching candidate is shown after the cursor as ghost text in ANSI 256 color 244 (assumption: "lighter gray"). Candidates: command names; after `/effort ` the values `low`, `medium`, `high`; after `/mode ` `auto`, `manual`; after `/model ` the configured `provider/model` ids. With an empty argument the ghost text is the hint `[low|medium|high]` / `[auto|manual]` / `[provider/model]`; once an argument prefix is typed it is the remainder of the first candidate with that prefix (e.g. `/effort m` → `edium`). `Tab` or `Right` accepts a completion; ghost text is never sent or stored. Hints are not accepted.
- R20. Input history: `Up` / `Down` cycle through the submitted inputs (messages and slash commands), newest first, when the input is empty or the cursor is on its first line (`Up`) / last line (`Down`); otherwise they move the cursor as before. Going past the newest entry restores the unsent draft. Consecutive duplicates are stored once; at most 500 entries (assumption); kept in memory only (never written to disk), and kept across `/new`. Recalled text can be edited and re-sent.
- R21. Copy-on-select: the program enables mouse capture. Dragging with the left button over the conversation highlights the selection (reverse video); on release the selected plain text (no ANSI codes, trailing spaces trimmed per line, lines joined by `\n`) is copied to the clipboard via OSC 52 and, when one of `wl-copy`, `xclip`, `xsel` exists, also via that tool. A click without drag copies nothing. Afterwards the selection is cleared and a one-line notice `N characters copied` (N = number of runes) appears directly above the input in RGB #B1B9F9 (assumption: Claude Code's light blue) for 3 s. Text is taken from the redacted rendering, so a secret never reaches the clipboard.
- R22. Paste: a bracketed paste (`tea.KeyMsg.Paste`) that contains a newline or is longer than 80 characters (assumption) is not inserted; the input shows `[Pasted text #N +L lines]` (L = line count; `[Pasted text #N · C chars]` for a single line), N counting up per session. On send the placeholder is replaced by the full text for the model; the conversation and the history show the placeholder. A placeholder the user edited or deleted is no longer expanded. Shorter pastes are inserted verbatim.
- R23. Scrolling: the mouse wheel scrolls the conversation 3 lines per notch; `Ctrl+Home` scrolls to the first line of the whole conversation and `Ctrl+End` to the last; with an empty input `Home` / `End` do the same. New output follows only while the view is at the bottom (unchanged).
- R24. Thinking indicator: while the session is busy, one line above the input reads `✻ Thinking… (12s · ↓ 1.2k tokens · esc to interrupt)`: glyph and verb in RGB #D77757 (assumption: Claude's red-orange), the parenthesis in color 244. The tokens are the output tokens of this turn so far (session `out` now minus `out` at submit, subagents included, `1.2k` from 1,000); they come from API `usage` or the chars/4 estimate, so they grow per model response, not per streamed character, and the part `↓ N tokens ·` is omitted while N is 0. The verb is `Working…` while a tool call of the main agent has no result yet. Elapsed time counts from the submit of the turn: `Ns` below 60 s, then `Nm SSs`. The line redraws every second while busy and disappears when the turn ends; no timer runs while idle. Static glyph, no animation (assumption).
- R25. Subagent display: for every subagent the conversation gets one live block, created when it first appears and updated in place: `● Subagent a1 (task)` and below it `  ⎿  ` + its latest tool call as `Name(summary)` (grey), then `  ⎿  Running… (3 tool uses · 1.2k tokens · 14s)` (orange `Running…`, grey rest). When it ends the last line becomes `Done` (green), `Failed` (red) or `Cancelled` (grey) with the final counts and total time; tokens are shown as `1.2k` from 1,000. The `spawn_agent` call and its result, and the other tool calls of subagents, are no longer rendered as separate blocks (their transcript stays in `/agents <id>`); approval prompts are unchanged. The panel above the input (R9) stays, one line per running subagent: `● a1 task (14s · 1.2k tokens)`, bullet in #D77757, rest in color 244. Times and tool-use counts are tracked in the TUI from events and its clock; `internal/agent` is not changed.

## 3. Changed requirements

- R15 (status): was one status line `cwd · provider/model · effort · mode · tokens in / out / total · context size` → now two rows under the input. Row 1 (default color): `cwd · provider/model · effort · mode`. Row 2 (color 244): `in N · out N · total N · ctx used[/limit]`. Same data, same shortening of a too long cwd on row 1, same dimmed effort for `reasoning:false`, still no cost, price or currency.
- R15 (conversation): was bordered tool blocks, `> ` plain user lines, unmarked assistant text → now Claude Code style, no borders: user message `> text` on a shaded full-width background (ANSI 236); assistant text introduced by `● ` with continuation lines indented 2 spaces; tool call `● Name(summary)` with the tool name bold, where summary is `command` for `bash`, `path` for the file tools, `pattern` for `glob`/`grep`, `task` for `spawn_agent` and compact JSON (max 200 runes) otherwise, prefixed `[id] ` for subagents; its result below as `  ⎿  ` + first line, further lines indented 5 spaces, collapsed to 3 lines plus `… +N lines (ctrl+o to expand)`; the tool `●` is dim while no result exists, green on success, red on error; error blocks are red; one blank line between blocks. Subagents render as in R25, the busy line as in R24. Fenced code stays syntax-highlighted, the input box keeps its rounded border.
- R9 (panel): was `id running task tokens` per running subagent → now the R25 format with elapsed time.
- R15 (completion): was a line of matching command names over the viewport on `/` → unchanged, and now accompanied by R19 ghost text.
- R11 (`/help`) and `yip --help` additionally list the keys `Up`/`Down` history, `Ctrl+Home`/`Ctrl+End`, mouse selection copies, `Tab` accepts a suggestion.

## 4. Removed requirements

- R15: the `in N / out N / total N` slash format and the bordered tool-block rendering (replaced above).

## 5. Constraints & environment changes

- Dependencies (both already in `go.sum` as indirect, no new download): `github.com/atotto/clipboard v0.1.4` and `github.com/aymanbagabas/go-osc52/v2 v2.0.1` become direct. `atotto/clipboard` runs `wl-copy`/`xclip`/`xsel` via `os/exec`; this is the only exec besides `internal/tools/bash.go`; it is allowed because the binaries are fixed and the text goes via stdin; `CLAUDE.md` (outside its Delegation section) is updated to say so.
- Optional tools, not required for build or test: `command -v wl-copy || command -v xclip || command -v xsel`.
- The clipboard writer and the clock are injectable in `Model`; no test touches the real clipboard, runs an external tool or contacts a host (R18 unchanged).
- Privacy: history and paste buffers live in memory only; yipyip still writes no files. No new secret env vars, no new live systems.
- Protected as in v1 (`prompts/`, `.claude/`, `README.md` §1–9, `Plan.md`, `versions/`, `scripts/verify/`).

## 6. Acceptance criteria

- [ ] `make check` exits 0 (v1 tests still pass, adapted where R15 changed) [all]
- [ ] `go test ./internal/tui -run Status`: with usage 100/20 the output has two rows; row 1 holds cwd, `provider/model`, effort, mode and none of `in `, `out `, `ctx`; row 2 holds `in 100`, `out 20`, `total 120`, `ctx` and renders with `38;5;244` under a forced 256-color profile; no `cost`, `$`, `price` [R15]
- [ ] `go test ./internal/tui -run Suggest`: `/eff` → ghost `ort`; `/effort ` → `[low|medium|high]`; `/effort m` → `edium`; `/model ` offers configured ids; Tab on `/effort m` gives input `/effort medium`; multi-line input or cursor not at end → no ghost; ghost uses `38;5;244`; the sent message never contains ghost text [R19]
- [ ] `go test ./internal/tui -run History`: Up/Down order, draft restored, duplicate collapse, cap 500, Up inside multi-line input moves the cursor, history survives `/new`, a temp HOME and start dir contain no new file afterwards [R20]
- [ ] `go test ./internal/tui -run Copy`: press, motion and release `tea.MouseMsg` over known text puts exactly that plain text into the fake clipboard; the view shows `N characters copied` with N = rune count, in `38;2;177;185;249` under a forced TrueColor profile, gone after 3 s on the fake clock; a click without motion copies nothing; a test with `SECRET123` shows it is never copied [R21, R16]
- [ ] `go test ./internal/tui -run Paste`: a paste with 3 lines shows `[Pasted text #1 +3 lines]` and not its content in the input; after Enter the fake provider's request contains the full text while the conversation and history show the placeholder; an edited placeholder is sent literally; a 10-character paste is inserted verbatim [R22]
- [ ] `go test ./internal/tui -run Scroll`: wheel up/down moves 3 lines; `Ctrl+Home` shows the first line of a 200-line conversation, `Ctrl+End` the last; Home/End do the same only with an empty input; streaming output does not snap back when scrolled up [R23]
- [ ] `go test ./internal/tui -run Render`: user, assistant, tool-call (`● bash(ls)`, `  ⎿  `), collapsed result (`… +N lines (ctrl+o to expand)`) and error blocks match the layout above, no border characters `╭│╰` outside the input box [R15]
- [ ] `go test ./internal/tui -run Thinking`: busy session shows `✻ Thinking… (0s · esc to interrupt)`, after 75 s on the fake clock `1m 15s`, after a fake response with 1,234 output tokens `↓ 1.2k tokens`, and tokens of an earlier turn are not counted; verb `Working…` with an open tool call; glyph/verb render with `38;2;215;119;87`, the parenthesis with `38;5;244`; line gone after the turn ends; no tick command is scheduled while idle [R24]
- [ ] `go test ./internal/tui -run Subagent`: with scripted subagent events one block `● Subagent a1 (task)` exists (updated, not duplicated), shows the latest tool call and `Running… (2 tool uses · 1.2k tokens · 14s)`, ends as `Done`/`Failed`/`Cancelled` in the right colors; no `spawn_agent` block and no separate `[a1]` tool blocks; the panel line has the R25 format; `/agents`, `/agents <id>`, `/agents kill` still pass [R25, R9]
- [ ] `go test ./internal/tui -run Help` and `go run ./cmd/yip --help` mention the new keys [R11]
- [ ] `go test ./... -run Secret` exits 0 [R16]
- [ ] `grep -n 'WithMouseCellMotion' cmd/yip/main.go` matches [R21, R23]
- [ ] `git diff --stat d3eeba4..HEAD -- internal/agent internal/tools internal/provider internal/config` prints nothing (d3eeba4 = last v1 commit) [scope]

## 7. Out of scope

Persisted history or `Ctrl+R` search; word/line double- and triple-click selection; selecting inside the input box; diff view for `edit_file`; animated spinner; image paste; themes; right-click paste; macOS/Windows clipboard; any change to agent, tools, provider or config.
