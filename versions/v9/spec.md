# yipyip v9: /clear in place, last tab replaced, rendered markdown

## 1. Goal

`/clear` should empty the current chat instead of opening a new tab, closing the last tab should leave a clean tab instead of quitting, and assistant text should be shown rendered (tables, headings, lists, emphasis) like Claude Code and Obsidian instead of as raw markdown source.

## 2. New requirements

- R73. `/clear` resets the current tab in place: the transcript is emptied, the tab gets a fresh empty session (same model, effort and mode; running turn, subagents, pending approval/question cancelled or denied as on tab close), the tab count does not change, input history and the tab's saved file are untouched. It prints one info line `cleared`. `/clear` while idle and while a turn runs both work.
- R74. Closing the last remaining tab (`×` click or `Ctrl+W`) discards that tab without saving it and shows one new empty tab (fresh session, empty transcript); the app keeps running. `/exit`, `/quit` and `Ctrl+C` quit as before.
- R75. Assistant blocks are rendered from markdown by an own stdlib-only renderer (`internal/tui/markdown.go`), applied after redaction, before width wrapping. Supported: tables (rows with a `|---|` separator line; box-drawn borders, columns padded to the widest cell, header bold, column alignment from `:---:`; when wider than the viewport the cells wrap, never overflow), headings `#`-`######` (bold, no `#` marks), `**bold**`, `*italic*`/`_italic_`, `` `inline code` ``, `~~strike~~`, bullet lists (`-`, `*`, `+` shown as `•`, nesting kept) and numbered lists, blockquotes `>` (shown with a `│` bar), horizontal rules (a full-width line), links `[text](url)` (shown as `text (url)`). Markup characters are not shown for what is rendered.
- R76. Fenced code blocks keep the existing chroma highlighting (`highlightFences`); text inside fences and inside inline code is never interpreted as markdown. Incomplete markdown (a streaming table without a separator yet, an unclosed `**` or fence) renders as plain text without error, and the finished text renders fully. User blocks, tool blocks, errors and info lines stay as they are.
- R77. Copy-on-select and the clipboard copy what is shown (rendered view text, redacted), unchanged in mechanism.

## 3. Changed requirements

- `/new` and `/clear` (alias, README/`/help`): was `/clear` is an alias of `/new` (new tab) → now `/clear` is its own command (R73); `/new` still opens a new tab. `/help` text is updated.
- Closing the last tab (v-tabs): was save then quit → now R74.

## 4. Removed requirements

none

## 5. Constraints & environment changes

none: standard library plus the existing modules only, no new packages; `transport.go`, `pathguard.go`, `bash.go`, `approval.go`, `redact.go` are not changed.

## 6. Acceptance criteria

- [ ] `go test ./internal/tui -run Clear -count=1 -v` exits 0: `/clear` keeps the tab count, empties the blocks, replaces the session, cancels a running turn, keeps the input history; `/new` still adds a tab [R73].
- [ ] `go test ./internal/tui -run CloseLastTab -count=1 -v` exits 0: `Ctrl+W` and the `×` click on the only tab leave exactly one empty tab, no `tea.Quit`, nothing saved for the closed tab; with 2 tabs closing still removes one [R74].
- [ ] `go test ./internal/tui -run Markdown -count=1 -v` exits 0 with golden-style tests (ANSI stripped): a table renders with box borders, padded and aligned columns, wraps at a 40-column width with no line wider than 40; headings, bold/italic/code/strike, nested bullets, numbered lists, quotes, rules and links render without markup characters [R75].
- [ ] Same run: fenced code stays highlighted and its `|`, `#`, `**` are untouched; inline code content is not interpreted; a half-streamed table, unclosed `**` and unclosed fence render as plain text [R76].
- [ ] `go test ./e2e -run Markdown -count=1` exits 0: a scripted model answers with a table and the rendered view contains the borders and no `|---|` line; selecting text copies the rendered, redacted text [R75, R77].
- [ ] `grep -n '/clear' internal/tui/help.go` shows an entry not described as an alias of `/new` [R73].
- [ ] `make check` (from `Plan.md`) exits 0: earlier versions keep working.

## 7. Out of scope

- Rendering markdown in user input, tool results or subagent blocks; images, HTML in markdown, footnotes, task-list checkboxes, reference-style links; clickable links; a config switch for raw mode.
- Changing `/new`, Ctrl+T, the tab limit or session saving on exit.
