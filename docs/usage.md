# yipyip usage

yip is a terminal coding assistant. Start it in a project directory; it reads,
searches and edits files and runs shell commands only inside that directory.

## Install

```sh
make install
```

`make install` builds the binary as `yip` into `$(go env GOBIN)` or, when GOBIN
is empty, `$(go env GOPATH)/bin` (usually `~/go/bin`). Make sure that directory
is on your `PATH`:

```sh
export PATH="$HOME/go/bin:$PATH"
```

Then run `yip` from any project directory. `yip --version` and `yip --help`
print the version and the command and key reference and exit.

## Configuration

yip reads two JSON files and merges them key by key:

1. `~/.config/yipyip/config.json` (global)
2. `yipyip.json` in the start directory (project). Its values win; nested
   objects such as `provider` are merged recursively.

A missing file counts as empty. Invalid JSON stops yip with an error.
[`config.example.json`](../config.example.json) shows a complete example.

Top-level keys:

- `default_model`: `provider/model` used at start. If absent or unknown, the
  first configured model is used.
- `max_subagents`: maximum number of subagents at once (default 4).
- `auto_compact`: compact the conversation automatically (default `true`).
- `provider`: an object keyed by provider id. Each provider has `name`,
  `options.baseURL` (the API base URL) and `options.apiKey`, and a `models`
  object keyed by model key. A model has:
  - `name`: display name.
  - `reasoning`: whether the model accepts a reasoning effort (default `true`).
  - `limit.context`: context window size in tokens, used for `ctx used/limit`.
  - `cost`: accepted for compatibility and ignored.

Secrets are never written in the files. Use `{env:NAME}` to read a value from
an environment variable:

```json
"apiKey": "{env:OPENROUTER_API_KEY}"
```

`NAME` is the variable name. If the variable is unset or empty, yip exits with
status 1 and names the variable (never its value). Export the variable in your
shell before starting yip:

```sh
export OPENROUTER_API_KEY=...   # set it in your shell, never in a file
```

## Modes

- **Auto** (default): `bash`, `write_file`, `edit_file` and `spawn_agent` run
  without asking.
- **Manual**: the same tools need your approval. The prompt offers `y` (allow
  once), `n` (deny; Esc also denies) and `a` (always allow this tool for the
  rest of the session).

Switch with `Shift+Tab`, or with `/mode [auto|manual]` (no argument toggles).
The first status row shows the mode as `auto (shift+tab to toggle)`, with
`auto` highlighted; in manual mode the word `manual` is shown in the default
colour.

## Slash commands

| Command | Description |
| --- | --- |
| `/model [provider/model]` | pick a model from a list, or set it directly |
| `/effort [low\|medium\|high]` | show or set the reasoning effort |
| `/mode [auto\|manual]` | switch the approval mode |
| `/agents [id \| kill id]` | list subagents, show one, or cancel one |
| `/init` | write or update `AGENTS.md` in the start directory |
| `/compact` | summarize the conversation to free context |
| `/undo` | undo the last turn's file changes |
| `/new` | start a fresh session |
| `/clear` | alias of `/new` |
| `/exit` | quit |
| `/quit` | alias of `/exit` |
| `/help` | show the command and key help |

Type `/` and press `Tab` or `Right` to accept the grey suggestion that appears
while you type a command (see [Input](#input)).

## Input

- **Suggestions**: typing `/` shows the matching slash command as grey ghost
  text after the cursor. `Tab` or `Right` accepts it. Mode and effort values
  are suggested the same way.
- **History**: `Up` and `Down` recall earlier inputs. History is kept in
  memory only (nothing is written to disk), survives `/new` within the same
  run, and keeps at most 500 entries.
- **Large pastes**: a long paste is shown as a placeholder such as
  `[Pasted text #1 +40 lines]`. The placeholder stands for the full text, and
  the full text is sent to the model.
- **Queue**: while the agent is busy, `Enter` queues the message. Queued
  messages are shown dimmed above the input and are sent together as the next
  turn when the current one ends. `Esc` or `Ctrl+C` cancels the running turn
  and moves the queued messages back into the input. `Up` on an empty input
  pulls them back for editing.

## Keys

| Key | Action |
| --- | --- |
| Enter | send the message; while the agent is busy, queue it |
| Alt+Enter | new line in the input |
| Esc | cancel the running turn (queued messages return to the input); deny a pending approval |
| Shift+Tab | toggle auto/manual mode (auto is the default) |
| Ctrl+C | twice: quit (the first press cancels the running turn) |
| Tab / Right | accept a suggestion or complete a command |
| Up / Down | input history (Up first restores queued messages) |
| Ctrl+O | expand or collapse tool results |
| PgUp / PgDn | scroll |
| Mouse wheel | scroll 3 lines |
| Ctrl+Home / Ctrl+End | scroll to the first/last line (Home/End with an empty input) |
| Mouse drag | select text; releasing copies it to the clipboard |

### Scrolling and copying

- The mouse wheel scrolls the conversation 3 lines per event. `PgUp` and
  `PgDn` scroll by a page, and `Ctrl+Home` and `Ctrl+End` jump to the first
  and last line. With an empty input, `Home` and `End` do the same.
- Drag with the left mouse button to select text. Releasing the button copies
  the selection: yip sends it with OSC 52 and, when installed, to `wl-copy`,
  `xclip` or `xsel`. The status line then shows `N characters copied`.
- yip captures the mouse, so the terminal cannot select text on its own. Hold
  `Shift` while dragging to use the terminal's own selection.
- Copied text is the redacted screen text. Secrets are never copied.

## Conversation

- Your messages start with `> `. The agent's replies and its tool calls are
  marked with `●`. Each tool result follows a `⎿` marker and is collapsed to
  3 lines with a hint such as `… +12 lines (ctrl+o to expand)`; `Ctrl+O`
  expands or collapses all results.
- While a turn runs, a busy line shows the elapsed time and the output tokens
  so far: `✻ Thinking… (12s · ↓ 1.2k tokens · esc to interrupt)`. It reads
  `✻ Working…` while a tool runs.
- Each turn ends with a finished marker: `✻ Worked for 1m 15s`, or
  `✻ Interrupted after 12s` when you cancelled it.

## Subagents

The model can start subagents with `spawn_agent` (at most `max_subagents` at a
time).

- Each subagent has one live block in the conversation:
  `● Subagent a1 (task)`, its latest tool call, and a state line
  `Running… (N tool uses · tokens · time)` that ends as `Done`, `Failed` or
  `Cancelled`.
- The subagent panel lists each one on a line such as
  `● a1 task (14s · 1.2k tokens)`.
- `/agents` lists them, `/agents <id>` shows the subagent's transcript, and
  `/agents kill <id>` cancels one.

## Undo, compaction and the status line

`/undo` reverts the files written by the last turn: restored files get their
earlier content back and files created in that turn are removed. Shell commands
cannot be reverted; `/undo` lists the bash commands of the undone turn so you
can check their effects by hand.

`/compact` replaces the conversation with a summary. With `auto_compact` on,
yip compacts automatically when the last prompt filled at least 80% of the
model's context limit.

The status area has two rows under the input:

```text
<cwd> · <provider/model> · <effort> · auto (shift+tab to toggle)
in N · out N · total N · ctx used/limit
```

Row 1 shows the working directory, `provider/model`, the reasoning effort and
the mode. When the row is too wide, the working directory is shortened from the
left. Row 2 shows the session token totals and context use (`ctx used/limit`,
or `ctx used` when the limit is unknown). No costs are shown.

## Security

- Network traffic goes only to the provider hosts configured in your config
  files. yip has no web tools.
- File tools (`read_file`, `write_file`, `edit_file`, `glob`, `grep`) are
  confined to the start directory; paths that escape it are refused.
- `bash` is not confined to the start directory. In auto mode (the default)
  commands run without asking; in manual mode each command needs your
  approval. Review commands before approving them, and switch to manual mode
  (`Shift+Tab`) if you want to approve each one.
- API keys are read only from environment variables. They are never shown in
  the UI or in errors, and yip writes no logs, history or config files.
