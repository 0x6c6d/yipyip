# yipyip

## 1. Overview

yipyip is a terminal UI coding assistant written in Go, in the style of Claude Code and opencode. It talks to OpenAI-compatible chat APIs, can read, search and edit files and run shell commands in the directory it was started in, and can spawn subagents and control them. It has no web tools, and its own network traffic goes only to the configured API providers. It runs in a manual mode (approval before each action) or an auto mode, and shows the working directory, model, effort and session token usage at all times. After `make install` it runs from any directory as `yip`.

## 2. Core requirements

- R1. Config: `~/.config/yipyip/config.json` (opencode-style `provider` map, optional `default_model`, `max_subagents`, `auto_compact`) is loaded; a `yipyip.json` in the start directory overrides it key by key. `{env:NAME}` in any string value is replaced from the environment. If a referenced variable is unset or empty, startup fails with a message naming the variable (never a value) and exit code 1. A `cost` field in a model entry is accepted and ignored.
- R2. Provider client: for a provider with `baseURL`, requests go to `POST <baseURL>/chat/completions` with `Authorization: Bearer <key>`, `stream: true`, `stream_options.include_usage: true`, tool definitions, and `reasoning_effort` set to the current effort. The SSE stream is parsed into text deltas, tool calls (including arguments split across chunks) and a final usage object. HTTP errors (4xx/5xx) and a dropped stream are shown in the conversation without crashing the program.
- R3. Network restriction: all outgoing HTTP from yipyip uses one transport that refuses any request whose host is not the host of a configured provider `baseURL`. The agent has no web fetch or search tool. The `bash` tool is not sandboxed (a human-equivalent shell).
- R4. File tools: `read_file`, `write_file`, `edit_file` (exact, unique `old_string` replaced by `new_string`; error if missing or ambiguous), `glob` (supports `**`), `grep` (regular expression, optional glob filter, results as `path:line:text`). Paths are resolved against the start directory, symlinks are resolved, and any path that ends up outside the start directory is rejected with an error returned to the model.
- R5. `bash` tool: runs `bash -c <command>` in the start directory, returns combined output and exit code, default timeout 120 s (parameter `timeout_seconds`, maximum 600), output truncated to 30,000 characters with a visible marker.
- R6. Agent loop: after a user message the main agent calls the model, runs requested tools, feeds results back, and repeats until a reply without tool calls or 50 iterations (assumption). `Esc` cancels the running turn, including running tools and subagents of that turn; the UI stays usable.
- R7. Modes: `manual` (default at startup) and `auto`. In manual mode every `bash`, `write_file`, `edit_file` and `spawn_agent` call shows a prompt `y / n / a (always for this tool this session)` before running; `n` returns "denied by user" to the model. Reads, `glob`, `grep`, `wait_agent` and `cancel_agent` never prompt. In auto mode nothing prompts. `Shift+Tab` and `/mode [auto|manual]` switch the mode and the status line shows it.
- R8. Subagents: tool `spawn_agent(task, model?)` starts a subagent in its own goroutine with a fresh context, the main tool set without `spawn_agent` (no nesting), and returns an id immediately. `wait_agent(id)` blocks until that subagent finishes and returns its final text. `cancel_agent(id)` stops it. At most `max_subagents` (default 4) run at once; further spawns return an error to the model. A subagent's tool approvals in manual mode appear in the same prompt, labelled with its id. Its token usage is added to the session total.
- R9. `/agents` lists subagents (id, task, status running/done/failed/cancelled, tokens); `/agents <id>` shows that subagent's transcript; `/agents kill <id>` cancels it. A panel above the input shows running subagents.
- R10. `/model`: without argument opens a picker of all configured `provider/model` entries; `/model <provider/model>` sets it directly; an unknown name shows an error and keeps the current model. `/effort low|medium|high` sets the effort (default `medium`); without argument it shows the current value. If the model entry has `"reasoning": false`, effort is kept but not sent and shown dimmed (assumption).
- R11. `/new` (alias `/clear`) discards conversation, subagents, undo history and token counters and starts a fresh session. `/exit` (alias `/quit`) and double `Ctrl+C` quit with exit code 0 and restore the terminal. `/help` lists all commands. Unknown commands show an error and are not sent to the model.
- R12. `/init`: asks the agent to inspect the repository and write or update `AGENTS.md` in the start directory with build/test commands and architecture notes; existing content is updated, not discarded.
- R13. `/compact`: replaces the history with a model-written summary, keeping the system prompt and the last 2 turns verbatim. It also runs automatically when the last prompt size reaches 80% of the model's `limit.context` (unless `auto_compact` is `false`). The session token total is not reduced by compaction.
- R14. `/undo`: restores every file changed by `write_file`/`edit_file` (by the main agent or subagents) during the last turn to its state before that turn (files created in the turn are deleted) and removes that turn from the conversation. It can be repeated back to the session start. It lists the `bash` commands of the undone turn as not reverted.
- R15. UI: Bubble Tea program with a scrolling conversation, a bordered multi-line input, the subagent panel, and one status line under the input showing `cwd · provider/model · effort · mode · tokens in / out / total · context size`. Assistant text streams in; tool calls render as blocks showing name and arguments and a collapsible result; fenced code blocks are syntax-highlighted; typing `/` shows matching commands. Token numbers come from API `usage`; if a response has none, they are estimated as characters / 4 (assumption). The UI shows no cost, price or currency anywhere.
- R16. Secrets: API key values never appear in the UI, logs, error messages, tool results or on disk written by yipyip.
- R17. Install: `make install` builds the binary and installs it as `yip` into `$(go env GOBIN)` or, if empty, `$(go env GOPATH)/bin`. `yip --version` prints `yip <version>` and `yip --help` prints usage, both without a config or API key. `yip` uses the shell's current directory as the start directory, so it works from any directory.
- R18. Testability: all tests run offline against an `httptest` fake provider; no test or build step contacts a real provider.

## 3. Flow

1. User runs `yip` in a project directory; config is loaded and `{env:...}` references resolved; the default model is selected, mode is `manual`, effort is `medium`.
2. The UI opens with the status line showing cwd, model, effort, mode and zeroed token counters.
3. User types a message; it is sent with the system prompt and the tool definitions to the provider; the reply streams into the conversation.
4. If the reply contains tool calls, yipyip asks for approval where required (manual mode), runs the tool, adds the result and calls the model again; repeat until a reply has no tool calls.
5. The main agent may spawn subagents; they run in parallel, appear in the panel, and return a final summary to the main agent.
6. Token counters and context size update from each response's usage.
7. The user can use slash commands at any time, or press `Esc` to interrupt.
8. `/exit` ends the session.

## 4. Tech stack

- Go 1.27 (`go 1.27` in `go.mod`; installed toolchain is go1.27.2); module path `yipyip`
- `github.com/charmbracelet/bubbletea` v1.3.10
- `github.com/charmbracelet/bubbles` v0.21.1 (textarea, viewport) (assumption: v1.0.0 is for a different Bubble Tea major, so v0.21.1 is used)
- `github.com/charmbracelet/lipgloss` v1.1.0
- `github.com/alecthomas/chroma/v2` v2.27.0
- Standard library for everything else: `net/http`, `encoding/json`, `os/exec`, `bufio`, `context`, `testing`, `net/http/httptest` (own SSE parser, own `**` glob; no SDK, no other third-party packages) (assumption)
- GNU Make for `check` and `install`

Layout (assumption): `cmd/yip` (main), `internal/config`, `internal/provider`, `internal/agent`, `internal/tools`, `internal/tui`, `e2e`.

## 5. Constraints & out of scope

- Out of scope for v1: native Anthropic and OpenAI-Responses protocols, session resume/persistence, MCP, plugins, image input, web tools of any kind, telemetry, auto-update, Windows and macOS support (Linux only).
- Network (high-risk, security): only the configured provider hosts are reachable by yipyip's own HTTP; tools for web access must not exist; keys come only from environment variables via `{env:NAME}`; keys are never printed or stored.
- Security (high-risk): file tools are confined to the start directory (symlink-safe); `bash` is intentionally unrestricted and must always go through the approval prompt in manual mode.
- Data/privacy (high-risk): yipyip writes no files except those the agent or the user causes (`AGENTS.md`, edited files) and its own config on user request; no logs containing prompts or keys are written to disk.
- Cost: no cost, price or currency is computed, stored or displayed anywhere (token counts only); `cost` fields in config are ignored.
- Must not be touched: `prompts/`, `.claude/`, the "Delegation workflow" section of `CLAUDE.md`. `Plan.md`, `versions/` and `scripts/verify/` are managed by the loop.

## 6. Environment

- Code lives in this repository root, `/home/sa/workspace/code/yipyip` (already a git repository; no new nested project folder, no `git init`).
- Required tools and checks:
  - Go ≥ 1.27: `go version | grep -q 'go1.27'`
  - Make: `make --version`
  - Git: `git --version`
  - gofmt: `command -v gofmt`
  - `~/go/bin` on `PATH` (so the installed `yip` is found from anywhere): `echo "$PATH" | tr ':' '\n' | grep -qx "$(go env GOPATH)/bin"`
- Secrets: `OPENROUTER_API_KEY` (name only). It is not needed to build or test; only to run yipyip against the real provider.
- Live systems the build may touch: the Go module proxy `https://proxy.golang.org` for `go mod download`, nothing else. The provider `https://openrouter.ai/api/v1` is used only when the user runs yipyip by hand; no build or test step may call it. Everything else is off limits.
- Manual testing: run yipyip by hand only against a free model of the provider (OpenRouter model ids ending in `:free`; add one under `models` in your own config, not in this repo). Do not use GLM 5.3 Flash.
- Example config the program must accept (as given by the user, `cost` omitted here but must be tolerated):

```json
{
  "max_subagents": 4,
  "auto_compact": true,
  "provider": {
    "openrouter": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "OpenRouter",
      "options": {
        "baseURL": "https://openrouter.ai/api/v1",
        "apiKey": "{env:OPENROUTER_API_KEY}"
      },
      "models": {}
    }
  }
}
```

## 7. Deliverables

- Go source: `cmd/yip/`, `internal/{config,provider,agent,tools,tui}/`
- `go.mod`, `go.sum`
- Unit tests next to the code and end-to-end tests in `e2e/` using an `httptest` fake provider
- `Makefile` with targets `build`, `check`, `install`
- `config.example.json` with the config above
- Usage notes appended to this README (install, config, commands, keys) under a new `## Usage` heading after the spec sections
- `AGENTS.md` is produced by `/init` at runtime, not shipped

## 8. Acceptance criteria

- [ ] `make check` (runs `gofmt -l .` expecting empty output, `go vet ./...`, `go test ./...`, `go build ./...`) exits 0 [R1–R18]
- [ ] Config tests cover: global config load, project override, `{env:X}` substitution, unset variable → error naming `X` and not containing a value, `cost` ignored; `go test ./internal/config` exits 0 [R1, R16]
- [ ] Provider tests against an `httptest` SSE server cover: request path, headers, body fields (`stream`, `stream_options`, `reasoning_effort`, tools), text deltas, tool-call arguments split across chunks, usage parsing, 500 and 401 responses, dropped stream; `go test ./internal/provider` exits 0 [R2]
- [ ] A test shows the HTTP transport rejects a request to a host not in the configured providers and allows the configured one; no tool named like web/fetch/search is registered; `go test ./internal/provider ./internal/tools` exits 0 [R3]
- [ ] Tool tests cover: `edit_file` missing/ambiguous/ok, `glob **`, `grep` output format, path escape via `..` and via symlink rejected, `bash` exit code, timeout and truncation; `go test ./internal/tools` exits 0 [R4, R5]
- [ ] Agent loop test with a scripted fake provider: tool call → result → final answer; iteration limit stops at 50; cancelling the context stops the turn within 2 s; `go test ./internal/agent` exits 0 [R6]
- [ ] Mode tests: manual mode prompts for `bash`/`write_file`/`edit_file`/`spawn_agent` and not for reads; `n` yields "denied by user"; `a` suppresses later prompts for that tool; auto mode never prompts; `/mode` and `Shift+Tab` toggle; `go test ./internal/agent ./internal/tui` exits 0 [R7]
- [ ] Subagent tests: spawn returns an id at once, 5th concurrent spawn with limit 4 errors, no `spawn_agent` in a subagent's tool list, `wait_agent` returns the final text, `cancel_agent` ends it, tokens added to the session total, approval prompt labelled with the id; `go test ./internal/agent` exits 0 [R8]
- [ ] TUI tests (Bubble Tea model driven with messages) cover `/agents`, `/agents <id>`, `/agents kill <id>`, `/model` picker and direct set and unknown name, `/effort`, `/new`, `/clear`, `/exit`, `/quit`, `/help`, unknown command; `go test ./internal/tui` exits 0 [R9, R10, R11]
- [ ] e2e test: `/init` against the fake provider produces an `AGENTS.md` file in a temp directory; `go test ./e2e -run Init` exits 0 [R12]
- [ ] e2e test: `/compact` shrinks the sent history to summary + system prompt + last 2 turns; auto-compact triggers at 80% of `limit.context` and not when `auto_compact` is false; `go test ./e2e -run Compact` exits 0 [R13]
- [ ] e2e test: `/undo` restores a modified file, deletes a created file, removes the turn from history, works twice in a row, and lists the `bash` commands as not reverted; `go test ./e2e -run Undo` exits 0 [R14]
- [ ] TUI render test: the status line contains cwd, the active `provider/model` id, effort, mode and `in`/`out`/`total` token counters, and the context size; after a fake response with usage 100/20 the counters show 100, 20 and 120; the rendered view, `/help` output and status line contain none of `cost`, `$`, `price`; `go test ./internal/tui -run Status` exits 0 [R15]
- [ ] A test with a fake key value `SECRET123` shows the string appears in no rendered view, error message, tool result or file written to a temp HOME; `go test ./... -run Secret` exits 0 [R16]
- [ ] `make install && cd / && command -v yip && yip --version` exits 0 and the output matches `^yip ` (run with `OPENROUTER_API_KEY` unset and no config file) [R17]
- [ ] `cd "$(mktemp -d)" && yip --help` exits 0 and prints usage listing the slash commands `/model /effort /init /new /exit /compact /undo /mode /agents /help` [R17, R10, R11]
- [ ] `grep -rn 'openrouter.ai' --include='*.go' .` finds matches only in `_test.go` files, and `grep -rn 'proxy.golang.org\|openrouter' Makefile` finds nothing, so no build or test step contacts a real provider [R18]

## 9. Nice-to-haves: explicitly not part of v1

- Native Anthropic and OpenAI-Responses protocol support
- Session persistence and `--resume`
- MCP servers and plugins
- Image input
- Custom slash commands and a per-project system-prompt file beyond `AGENTS.md` loading
- Per-subagent model and effort overrides in the UI
- Optional sandboxing of the `bash` tool
- Shell completion and man page
