# Plan: yipyip

_Core plan, valid for all versions; edited in place by the planner of each version (git is the
history). Phases, Definition of Done and requirement coverage live in `versions/vN/plan.md`._

## Scope
**Product scope so far:** v1 (README R1–R18): Go/Bubble Tea coding assistant `yip` for
OpenAI-compatible streaming APIs; `{env:NAME}` config, host-restricted HTTP, start-dir-confined
file tools, unrestricted `bash`, agent loop, manual/auto modes, subagents, slash commands,
token-only status line (never cost), `make install` as `yip`.
**Explicitly deferred (product level):** native Anthropic / OpenAI-Responses protocols, session
persistence, MCP, plugins, image input, web tools, telemetry, auto-update, Windows/macOS, custom
slash commands, per-subagent model/effort UI, bash sandboxing, completion/man page, config writes.
**Where the code lives / protected paths (no worker may touch):** code in the repo root
`/home/sa/workspace/code/yipyip` (module `yipyip`): `cmd/yip/`, `internal/{config,provider,agent,
tools,tui}/`, `internal/provider/providertest/` (shared fake provider), `e2e/`, `Makefile`,
`go.mod`, `go.sum`, `config.example.json`, `.gitignore`. Protected: `prompts/`, `.claude/`,
`Plan.md`, `versions/`, `scripts/verify/`; `README.md` sections 1–9 (the only allowed change is
appending a `## Usage` section at the end of the file); the "Delegation workflow" section of
`CLAUDE.md` (the rest of `CLAUDE.md` may be updated to match the code). No worker commits, runs
`git init`, or contacts any host except `proxy.golang.org` (via `go mod download`/`go get`).

## Architecture
Flow: `cmd/yip` → `config.Load` → `tui.New(session)` → `agent.Session.Submit` → `provider` SSE
events → approval gate → `tools.Registry.Run` → results into history → repeat; `tui` renders events.
- **cmd/yip**: responsibility: `--version` (`yip <version>`, `-ldflags -X main.version`, default
  `dev`) and `--help` (usage + slash commands) before any config load; start dir = symlink-resolved
  `os.Getwd()`; startup errors → stderr + exit 1; talks to: config, agent, tui. (since v1)
- **internal/config**: responsibility: load `~/.config/yipyip/config.json` (missing = empty) and
  `<startdir>/yipyip.json`, deep-merge objects key by key (non-object values replaced),
  substitute `{env:NAME}` in every string (unset/empty → error naming `NAME`, exit 1), parse
  `provider.<id>.options.{baseURL,apiKey}`, `models.<id>.{name,reasoning,limit.context}`,
  ignore `cost`/`npm`/unknown keys; `default_model`, `max_subagents` (4), `auto_compact` (true);
  owns the **Redactor** (all resolved secret values → `[redacted]`); talks to: nothing. (since v1)
- **internal/provider**: responsibility: `Client.Stream(ctx, req) <-chan Event` for
  `POST <baseURL>/chat/completions` (Bearer key if set, `stream:true`,
  `stream_options.include_usage:true`, `tools`, `reasoning_effort` unless `reasoning:false`);
  own SSE parser (`data:` lines, `[DONE]`, tool-call argument accumulation by `index`, usage in
  final chunk with empty `choices`); HTTP 4xx/5xx and dropped streams become `ErrorEvent`
  (redacted body excerpt, max 500 chars); **Transport**: the single `http.RoundTripper` used by
  every client, allowlist of `host:port` from configured baseURLs, `Proxy: nil`, rejects other
  hosts (also on redirects); `providertest`: scripted `httptest` SSE fake; talks to: config. (v1)
- **internal/tools**: responsibility: `Registry` with `read_file`, `write_file`, `edit_file`,
  `glob`, `grep`, `bash` (JSON schemas + `Run(ctx, name, args) Result`); **PathGuard** (resolve
  against start dir, `EvalSymlinks` of the deepest existing ancestor, reject outside); **Bash
  runner** (the only `os/exec` call site: `bash -c`, start dir, process group killed on
  timeout/cancel, combined output, 120 s default / 600 s max, 30,000-char truncation marker, env
  without the config's `{env:}` variables); pre-write hook for undo; never registers
  web/fetch/search tools; talks to: config (redactor). (since v1)
- **internal/agent**: responsibility: `Session` (history, model/effort/mode, token counters,
  turns), loop (≤50 iterations per turn, per-turn `context.Context` cancelled by Esc),
  **Approval gate** (manual mode prompts for `bash/write_file/edit_file/spawn_agent`, `y/n/a`,
  "always" set per tool per session, requests labelled with agent id), **Subagent manager**
  (`spawn_agent/wait_agent/cancel_agent`, goroutine each, cap `max_subagents`, no nesting,
  tokens added to session), **Undo journal** (in-memory pre-turn snapshots per turn, incl.
  subagent writes), compaction (manual + auto at 80% of `limit.context`), `/init` prompt,
  system prompt (+ `AGENTS.md` of the start dir if present); talks to: provider, tools. (v1)
- **internal/tui**: responsibility: Bubble Tea `Model` (alt screen): streaming viewport, tool
  blocks (name, args, collapsible result), chroma fences, bordered textarea, subagent panel,
  status line `cwd · provider/model · effort · mode · tokens in / out / total · context size`,
  slash commands + `/` completion, model picker, approval prompt, Esc / Shift+Tab / double
  Ctrl+C; all text passes the redactor; talks to: agent (events → `tea.Msg`). (since v1)
- **e2e**: responsibility: offline tests `Init`, `Compact`, `Undo`, `Secret` via `agent.Session`
  + tui dispatcher against `providertest` in temp dirs/HOME; talks to: agent, tui. (since v1)

## Commands
- install: `go mod download`
- test: `go test ./...`
- lint: `test -z "$(gofmt -l .)" && go vet ./...`
- build: `make build` (`go build ./...` plus `go build -o bin/yip ./cmd/yip`)
- check (runs all of the above, exit 0 = healthy): `make check` (`gofmt -l .` empty,
  `go vet ./...`, `go test ./...`, `go build ./...`; Makefile never names `proxy.golang.org`/`openrouter`)
- install binary: `make install` (binary `yip` into `$(go env GOBIN)`, else `$(go env GOPATH)/bin`)

## Model-Routing Policy
Tier per task via the `Agent` tool's `model`. Quality first, then cost: never pick a lower tier
if a wrong result costs more to redo. Precedence: a security or Consequential rule beats a cost
example.
- **haiku**: clear boundary, small diff, mechanically verifiable. Examples: `config.example.json`
  and `.gitignore`; Makefile targets `build/check/install`; `--version` flag; `/help` text and
  the README `## Usage` section.
- **sonnet** (default): multi-file features, moderate design judgment, new patterns. Examples:
  SSE parser + streaming client (using the opus transport), `glob`/`grep`/`read_file`, TUI views
  (status line, tool blocks, picker, panel, highlighting), slash command dispatch, `/compact`,
  `/init`, e2e tests.
- **opus**: architecture, security-sensitive code (auth, secrets, crypto, parsing untrusted
  input, shell/SQL/command construction), concurrency, cross-cutting refactors, anything touching
  a Consequential assumption. Examples: host-allowlist Transport; PathGuard; Bash runner;
  approval gate; Redactor/`{env:}` errors; subagent manager, turn cancellation; undo journal.
- **Shared-code rule:** security-critical logic lives only in shared, opus-built code:
  `internal/provider/transport.go`, `internal/tools/pathguard.go`, `internal/tools/bash.go` (sole
  `os/exec` site), `internal/agent/approval.go`, `internal/config/redact.go`. Modules that only
  call them and parse output (file tools, client, TUI) may be sonnet; the supervisor then runs
  the negative checks itself (`..`/symlink escape, foreign host, unprompted bash, `SECRET123`).

## Risks & Open Questions
Non-blocking; tasks touching an item get opus and negative-case verification.
- C: Transport allowlist compares exact `host:port` of configured baseURLs (default port by
  scheme), ignores `HTTP(S)_PROXY` (`Proxy: nil`), refuses redirect targets on other hosts; no
  other `http.Client`/`http.DefaultTransport` use anywhere (grep-checked).
- C: PathGuard: absolute or relative paths, `EvalSymlinks` on the deepest existing ancestor
  (new files allowed only if that ancestor is inside), start dir itself symlink-resolved;
  rejection is an error result to the model, never a panic.
- C: Secrets: every resolved `{env:}` value is registered with the Redactor and replaced by
  `[redacted]` in UI text, errors, tool results and HTTP error bodies; these variables are
  removed from the `bash` child environment; no file yipyip writes contains them.
- C: No log files, no history/session files, no config writes in v1; only tool-caused writes
  (`write_file`, `edit_file`, `AGENTS.md` via the agent) touch disk.
- C: Approvals: "always" (`a`) is per tool, per session, shared by main agent and subagents,
  reset by `/new`; switching to manual mode re-enables prompts for tools not in that set; the
  gate is the only path from a tool call to `Registry.Run` for gated tools.
- C: Undo snapshots are in memory only (lost on exit, unbounded size); undo restores bytes and
  mode, deletes files created in the turn, never reverts `bash` effects (listed instead).
- C: `bash` is intentionally unsandboxed; the process group is killed on timeout and on Esc.
- C: Public interface: config schema, tool names/arguments and slash commands as in README are
  treated as stable; renaming later is a breaking change.
- Live systems: only `proxy.golang.org` during the build; OpenRouter only when the user runs
  `yip` by hand with a `:free` model; a real host in tests or Makefile fails R18.
- Deps: bubbles v0.21.1 requires bubbletea v1.3.10 + lipgloss v1.1.0 (verified on the proxy);
  chroma v2.27.0 needs go ≥ 1.25. No other modules; servers that omit usage or reject
  `reasoning_effort` yield a chars/4 estimate or a shown HTTP error, never a crash.
- Testability: no teatest; TUI tests drive `Model.Update` and assert on `View()`.
