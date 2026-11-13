# yipyip

> **About this repo:** I built this to learn about AI agent loops, subagents and similar AI flows, not to write the code myself. The whole codebase is AI-written. My part was designing the supervisor/worker loop around it. Treat it as an experiment in agent workflows, not as hand-crafted code.

A terminal coding assistant in Go, in the style of Claude Code and opencode. It works with any OpenAI-compatible chat API.

yipyip (binary: `yip`) runs in your project directory. It reads and searches your code, edits files, runs shell commands and can hand work to subagents. You can approve every action by hand or let it run on its own. Network access is restricted to the hosts you configure, and secrets never reach the screen, the logs or the disk.

## Features

- **Any OpenAI-compatible provider**: OpenRouter, a local server, or any API with `/chat/completions` and streaming. Several providers and models side by side, switchable at runtime.
- **Coding tools**: `read_file`, `write_file`, `edit_file`, `glob`, `grep` and `bash`. Writes stay inside the start directory.
- **Two modes**: *auto* runs tools without asking, *manual* asks before every write, command or web call (`y` / `n` / `a` for always).
- **Subagents**: the model can spawn parallel workers with their own context. Live blocks and a panel show their progress.
- **Web access**: `web_fetch` reads one page as text. `web_search` uses the Brave Search API when you configure it.
- **Sessions and tabs**: up to 8 conversations side by side, autosaved, resumable with `yip --resume`.
- **Headless mode**: `yip -p "..."` runs one turn without the UI and prints the result, optionally as a JSON Lines trace, with exit codes for done, error, unanswered question, timeout and model-call limit.
- **Undo and compaction**: `/undo` reverts the file changes of the last turn, `/compact` summarizes the conversation (also automatic at 80% of the context window).
- **Skills**: put a `SKILL.md` in a folder and run it as `/<name>`.
- **Good terminal UI**: streaming output, markdown rendering with tables, syntax highlighting, mouse scrolling, copy on select, input history and a message queue.
- **Clean exit**: when yip exits it terminates every process its `bash` commands started, including background jobs, dev servers and watchers (see [Process cleanup](#process-cleanup)).
- **Safe by design**: host-restricted HTTP transport, secret redaction everywhere, a crash log that never contains prompts or keys.

## Requirements

- Go 1.27
- GNU Make
- An API key for an OpenAI-compatible provider
- Optional: `wl-copy`, `xclip` or `xsel` for clipboard copy (OSC 52 is used as well)

## Installation

Clone this repository, then:

```sh
make install
```

This builds the binary and installs it as `yip` into `$(go env GOBIN)`, or `$(go env GOPATH)/bin` when `GOBIN` is empty (usually `~/go/bin`). Put that directory on your `PATH`:

```sh
export PATH="$HOME/go/bin:$PATH"
```

To build only, without installing: `make build` writes `bin/yip`.

## Quick start

1. Create `~/.config/yipyip/config.json` (see [`config.example.json`](config.example.json)):

   ```json
   {
     "default_model": "openrouter/anthropic/claude-sonnet-4.5",
     "provider": {
       "openrouter": {
         "name": "OpenRouter",
         "options": {
           "baseURL": "https://openrouter.ai/api/v1",
           "apiKey": "{env:OPENROUTER_API_KEY}"
         },
         "models": {
           "anthropic/claude-sonnet-4.5": {
             "name": "Claude Sonnet 4.5",
             "limit": { "context": 200000 }
           }
         }
       }
     }
   }
   ```

2. Export the key and start `yip` in a project:

   ```sh
   export OPENROUTER_API_KEY=...
   cd ~/my-project
   yip
   ```

3. Type a request and press `Enter`.

Run `yip --help` for the command line, `/help` inside the app for all commands and keys.

```text
yip [--version] [--help] [--resume [id]] [-p prompt [flags]]

  --resume [id], -c [id]   resume a saved session of this directory (without id: pick one)
  -p, --prompt <text>      run one turn without the UI (see Headless mode)
```

### Slash commands

| Command | Description |
| --- | --- |
| `/model [provider/model]` | pick a model or set it directly |
| `/effort [low\|medium\|high]` | show or set the reasoning effort |
| `/mode [auto\|manual]` | switch the approval mode (`Shift+Tab` toggles) |
| `/agents [id \| kill id]` | list, inspect or cancel subagents |
| `/init` | write or update `AGENTS.md` for the project |
| `/compact` | summarize the conversation to free context |
| `/undo` | undo the last turn's file changes |
| `/new` / `/clear` | open a fresh tab / empty the current one |
| `/sessions [id \| rm id]` | list, load or remove saved sessions |
| `/skills` | list loaded skills |
| `/<skill> [args]` | run a skill |
| `/exit`, `/quit` | quit |

### Key bindings

| Key | Action |
| --- | --- |
| `Enter` / `Alt+Enter` | send (queued while busy) / new line |
| `Esc` | cancel the running turn, deny a pending approval |
| `Shift+Tab` | toggle auto / manual mode |
| `Ctrl+T` / `Ctrl+W` | new tab / close tab |
| `Ctrl+O` | expand or collapse tool results |
| `Ctrl+C` | press twice to quit |
| `PgUp` / `PgDn`, mouse wheel | scroll |

## Headless mode

`yip -p` runs one turn without the UI, in auto mode, and exits. Flags other than `--version`, `--help` and `--resume` apply only with `-p`.

```sh
yip -p "explain main.go"
echo "list the exported functions" | yip -p -
yip -p "fix the failing test" --json --timeout 10m > run.jsonl
```

Without `--json`, stdout gets the final text of the main agent. Progress lines (tool calls, approvals, retries) go to stderr. With `--json`, stdout gets one JSON record per line and the last line is a `result` record.

| Flag | Default | Effect |
| --- | --- | --- |
| `-p`, `--prompt <text>` | | run one turn with this prompt; `-` reads it from stdin; a skill such as `/review` works |
| `--json` | off | print the run as JSON Lines |
| `--answer <text>` | | answer a question the agent asks, in order; repeatable |
| `--approve <tool,...\|all>` | none | approve these tools in manual mode |
| `--dir <path>` | current directory | start directory |
| `--model <id>` | configured model | model for this run only |
| `--effort <low\|medium\|high>` | configured effort | reasoning effort for this run only |
| `--mode <auto\|manual>` | `auto` | approval mode; in manual mode, calls not covered by `--approve` are denied |
| `--timeout <duration>` | none | stop the run after this long, for example `30m` |
| `--max-model-calls <n>` | no limit | stop the run after n model calls |
| `--system-append <text>` | | add text to the system prompt |
| `--no-save` | off | do not save the session |
| `--transcript <file>` | | write the full transcript to a file inside the start directory |
| `--resume <id>` | | resume a saved session of this directory |

### Exit codes

| Code | Meaning |
| --- | --- |
| 0 | done |
| 1 | run error (setup failed, the turn reported an error, or a signal interrupted it) |
| 2 | usage error or crash |
| 3 | unanswered question (more questions than `--answer` values) |
| 4 | timeout |
| 5 | model-call limit reached |

### JSON Lines records

Every record has a `type`:

| Type | Content |
| --- | --- |
| `text` | text of an agent (`agent`, `text`) |
| `tool_call` | a tool call (`agent`, `name`, `args`) |
| `tool_result` | its result (`agent`, `name`, `ok`, `output`, truncated to 2000 characters) |
| `subagent` | subagent state (`id`, `state`, `task`, `model`) |
| `question` | a question from the agent (`agent`, `question`, `options`, `multiple`) |
| `approval` | an approval decision (`agent`, `tool`, `summary`, `decision`) |
| `retry` | a provider retry (`text`) |
| `error` | an error (`agent`, `text`) |
| `result` | the last record, see below |

The `result` record has these fields: `session_id` (empty when not saved), `exit_code`, `reason` (`done`, `error`, `question`, `timeout` or `max_calls`), `final_text`, `model_calls`, `subagents`, `tokens` (`in`, `out`), `duration_ms`, `tools` (calls per tool name) and `files_changed` (paths relative to the start directory).

Example: run a skill with limits and check how it ended.

```sh
yip -p "/loop-coding v2" --json --timeout 30m --max-model-calls 200 --no-save > run.jsonl; echo $?
tail -n 1 run.jsonl
```

Headless runs keep every security rule: network traffic only to the configured hosts, writes only inside the start directory (or `--dir`), secrets redacted in all output.

## Configuration

yipyip merges two JSON files key by key. A missing file counts as empty, invalid JSON stops startup with an error.

1. `~/.config/yipyip/config.json`: global
2. `yipyip.json` in the start directory: project, wins over the global file

| Key | Default | Effect |
| --- | --- | --- |
| `default_model` | first configured model | `provider/model` used at start |
| `effort` | `medium` | reasoning effort: `low`, `medium` or `high` |
| `mode` | `auto` | approval mode: `auto` or `manual` |
| `max_subagents` | `4` | maximum number of subagents running at once |
| `auto_compact` | `true` | compact automatically at 80% of the context window |
| `provider.<id>.name` | | display name |
| `provider.<id>.options.baseURL` | | API base URL; requests go to `<baseURL>/chat/completions` |
| `provider.<id>.options.apiKey` | | API key, preferably `{env:NAME}` |
| `provider.<id>.models.<key>.name` | | display name of the model |
| `provider.<id>.models.<key>.reasoning` | `true` | whether the model accepts a reasoning effort |
| `provider.<id>.models.<key>.limit.context` | unknown | context window in tokens, used for `ctx used/limit` and auto-compaction |
| `search.baseURL` | `https://api.search.brave.com` | Brave API host; `https` only (plain `http` for localhost) |
| `search.apiKey` | | required when `search` exists; enables `web_search` |

`default_model`, `effort` and `mode` are written back to the global file when you change them in the UI. Other keys, unknown keys and `{env:NAME}` references are left as they are.

### Secrets

Never put keys in the files. `{env:NAME}` in any string value reads an environment variable. If it is unset or empty, `yip` exits with status 1 and names the variable, never its value. Resolved secrets appear as `[redacted]` everywhere and are removed from the environment of `bash`.

### Web search

Add a `search` block to enable `web_search`. Without it, only `web_fetch` is available.

```json
{
  "search": {
    "baseURL": "https://api.search.brave.com",
    "apiKey": "{env:BRAVE_API_KEY}"
  }
}
```

### Skills

A skill is a folder with a `SKILL.md`: frontmatter with `name` and `description`, then the prompt body.

```markdown
---
name: review
description: review the current diff
---
Review the changes in the working tree and list problems.
```

Skills load from `~/.config/yipyip/skills/` and `<startdir>/.yipyip/skills/` (the project wins). Run one with `/review`. Built-in commands win over skills of the same name.

### Instructions (AGENTS.md)

yip reads a user-wide instructions file, `~/.config/yipyip/AGENTS.md` (next to `config.json` and `skills/`), and the project `AGENTS.md` in the start directory. Both go into the system prompt of the main agent and of subagents, in the UI and in `yip -p`, with no approval needed. The global file is added under `# Global AGENTS.md` before the project file, so project instructions come later and win on conflict.

Each file is cut to 30,000 characters and configured secret values are redacted. A missing, empty or whitespace-only file, or a path that is not a regular file, adds nothing and is not an error. The global file is read when a session starts and again on `/new` and `/clear`; it is not watched. `/init` never reads or writes the global file; it only writes the project `AGENTS.md`.

### Files yipyip writes

| Path | Content |
| --- | --- |
| `<startdir>/.yipyip/sessions/` | saved sessions (redacted), the newest 25 are kept |
| `<startdir>/.yipyip/crash.log` | crash records, no prompts or keys |
| `~/.config/yipyip/config.json` | model, effort and mode when you change them |
| the `--transcript` file | transcript of a headless run, inside the start directory |

A headless run saves its session like the UI does, unless you pass `--no-save`. It never writes `config.json`.

Add `.yipyip/` to your `.gitignore`.

## Security model

- Network traffic goes only to your provider hosts, the Brave host (while configured) and the host of an approved `web_fetch` URL. `web_fetch` refuses loopback, private, link-local and multicast addresses.
- File reads (`read_file`, `glob`, `grep`) work anywhere on the file system and are not approval-gated. Anything your user can read can reach the model's context.
- File writes are confined to the start directory.
- `bash` is **not** sandboxed. In auto mode commands run without asking. Use manual mode (`Shift+Tab`) if you want to review each one. Processes it starts are terminated when yip exits (see Process cleanup).
- Web content is treated as untrusted data, never as instructions.

### Process cleanup

When yip exits, it terminates every process its `bash` commands started, including background jobs (`cmd &`, `nohup`), dev servers and watchers, from the main agent and from subagents. Processes yip did not start are never touched.

- **When**: on every exit path: quitting the UI (`/quit`, `/exit`, Ctrl+C twice, Ctrl+D), closing the terminal or `kill <pid>` (SIGTERM/SIGHUP; the session is saved first), the end of a `yip -p` run (also after `--timeout`, `--max-model-calls`, an unanswered question or a signal), and a crash exit (after the crash record).
- **How**: running subagents are cancelled first, then the processes get SIGTERM, a 2 s grace, and SIGKILL for what is left. Exit is delayed by at most about 2.5 s, and is immediate when nothing is left.
- **Linux**: yip registers as a child subreaper, so processes that detach with `setsid` or a double fork are caught as well. On other platforms only the process groups of the commands are terminated.
- **Not covered**: `kill -9` of yip itself cannot be caught. Long-lived servers started by yip do not survive the session.

## Project structure

```text
cmd/yip/            entry point, flags, crash handling
internal/agent/     agent loop, approvals, subagents, compaction, undo
internal/config/    config loading, {env:} resolution, redaction
internal/provider/  streaming client, SSE parser, restricted HTTP transport
internal/session/   session store
internal/skills/    skill loader
internal/tools/     file, search, bash and web tools, path guard
internal/tui/       Bubble Tea interface
internal/crashlog/  crash log
e2e/                end-to-end tests against a fake provider
docs/usage.md       detailed usage reference
```

## Development

Tests run offline against an `httptest` fake provider. No real provider is contacted.

```sh
go test ./...        # all tests
go test ./e2e/...    # end-to-end only
make check           # gofmt, go vet, go test, go build; exit 0 = healthy
```

See [`docs/usage.md`](docs/usage.md) for the full reference: sessions, tabs, retries, subagents, markdown rendering and crash handling.
