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

- **Manual** (default): `bash`, `write_file`, `edit_file` and `spawn_agent` need
  your approval. The prompt offers `y` (allow once), `n` (deny; Esc also denies)
  and `a` (always allow this tool for the rest of the session).
- **Auto**: the same tools run without asking.

Switch with `Shift+Tab`, or with `/mode [auto|manual]` (no argument toggles).

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

Type `/` and press `Tab` to complete a command name.

## Keys

| Key | Action |
| --- | --- |
| Enter | send the message |
| Alt+Enter | new line in the input |
| Esc | cancel the running turn (or deny a pending approval) |
| Shift+Tab | toggle manual/auto mode |
| Ctrl+C | press twice within 2 seconds to quit (the first press cancels the running turn) |
| Tab | complete a slash command |
| Ctrl+O | expand or collapse tool results |
| PgUp / PgDn | scroll the conversation |

## Subagents

The model can start subagents with `spawn_agent` (at most `max_subagents` at a
time). A subagent panel lists each subagent with its id, state and task. Use
`/agents` to show them and `/agents kill id` to cancel one.

## Undo, compaction and the status line

`/undo` reverts the files written by the last turn: restored files get their
earlier content back and files created in that turn are removed. Shell commands
cannot be reverted; `/undo` lists the bash commands of the undone turn so you
can check their effects by hand.

`/compact` replaces the conversation with a summary. With `auto_compact` on,
yip compacts automatically when the last prompt filled at least 80% of the
model's context limit.

The status line shows, in order: the working directory, `provider/model`, the
reasoning effort, the mode, token counts (`in N / out N / total N`, session
totals), and context use (`ctx used/limit`, or `ctx used` when the limit is
unknown). Token counts are the only usage figures shown.

## Security

- Network traffic goes only to the provider hosts configured in your config
  files. yip has no web tools.
- File tools (`read_file`, `write_file`, `edit_file`, `glob`, `grep`) are
  confined to the start directory; paths that escape it are refused.
- `bash` is not confined to the start directory. In manual mode each command
  needs your approval; in auto mode it runs without asking. Review commands
  before approving them.
- API keys are read only from environment variables. They are never shown in
  the UI or in errors, and yip writes no logs, history or config files.
