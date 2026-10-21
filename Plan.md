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
- **cmd/yip**: `--version` (`yip <version>`, default `dev`) and `--help` (usage + slash commands +
  keys) before any config load; start dir = symlink-resolved `os.Getwd()`; startup errors → stderr +
  exit 1; v3 `--resume [id]` (alias `-c`): id reopens that session, no id + TTY → session picker,
  non-TTY/empty dir → fresh session, unknown id → `unknown session <id>` + exit 1; talks to:
  config, agent, session, tui. (v1; v2: auto-mode start, mouse, keys; v3: `--resume`)
- **internal/config**: load `~/.config/yipyip/config.json` (missing = empty) + `<startdir>/
  yipyip.json` deep-merge key by key, `{env:NAME}` (unset/empty → error naming `NAME`, exit 1),
  provider/model parsing, `max_subagents` (4), `auto_compact` (true), ignores `cost`/`npm`; owns
  the **Redactor** (resolved secrets → `[redacted]`); v3 settings write: exactly `model`/
  `effort`/`mode` into the user config — atomic, every other key preserved (unknown and
  `{env:NAME}` stay literal), never a resolved secret; talks to: nothing. (v1; v3: settings)
- **internal/provider**: `Client.Stream(ctx, req) <-chan Event` → `POST <baseURL>/chat/completions`
  (Bearer key if set, `stream:true`, `include_usage:true`, `tools`, `reasoning_effort` unless
  `reasoning:false`); own SSE parser; 4xx/5xx + dropped streams → redacted `ErrorEvent`s; v3 retry
  (client.go only): pre-first-byte failures (dial/TLS/write timeout, reset, 429, 5xx) → up to 10
  attempts, waits 1,2,4,8,16,32,60,60,60 s, same transport/body/host, `retrying (n/10) in <wait>`
  event, never after the first SSE byte or on other 4xx; **Transport** (unchanged in v3): the
  single `http.RoundTripper`, `host:port` allowlist, `Proxy: nil`, rejects other hosts + redirects;
  `providertest`: scripted `httptest` SSE fake (v3: scriptable reset/429/5xx); talks to: config.
  (v1; v3: retry)
- **internal/session** (v3): `Store` at `<startdir>/.yipyip/sessions`, `SessionData` (R29 fields),
  `Save`/`Load`/`List` (newest first)/`Delete`; atomic temp+`os.Rename`; every path through the
  `tools` PathGuard (`.yipyip` symlinked outside → refused); `version`≠1 or invalid JSON → error
  naming the file; unknown fields ignored; imports agent, never the reverse.
- **internal/tools**: `Registry` (`read_file`, `write_file`, `edit_file`, `glob`, `grep`, `bash`),
  **PathGuard** (`EvalSymlinks` of the deepest existing ancestor, reject outside), **Bash runner**
  (the only `os/exec` site, env without the `{env:}` variables), undo pre-write hook, never
  registers web/fetch/search; talks to: config (redactor). (v1; unchanged in v3 — `question`
  lives in `internal/agent`)
- **internal/agent**: `Session` (history, model/effort/mode, counters, turns) + v3 export
  accessors / restore setters; loop (≤50 iterations per turn, per-turn ctx, Esc cancels);
  **Approval gate** (`y/n/a`, "always" per tool per session, id labels); v3 **Question gate**:
  `question` (auto-approved, never gated) blocks on a TUI-fed reply channel — result: labels
  joined `, ` / free text / `cancelled`; **Subagent manager**, **Undo journal**, compaction,
  `/init`, system prompt (+`AGENTS.md`); talks to: provider, tools. (v1; v3: question, restore)
- **internal/tui**: Bubble Tea `Model` (alt screen, mouse): v3 **tabs** — up to 8 sessions, top
  bar `N:<title>` (current highlighted), per-tab conversation/counters/history/pastes/queue/turn,
  global model/effort/mode, `Ctrl+T/W/Tab/Shift+Tab`, `Alt+1`–8; Claude Code viewport (chroma,
  subagent blocks), textarea (suggest, history, pastes, queue), status rows, scroll,
  copy-on-select; slash commands + v3 `/sessions` (list/load/rm) + save of every open tab on exit;
  overlays: model picker, approval prompt, v3 resume picker + question overlay, v3 settings
  persist on selection change; alt+enter + v3 shift+enter newline; text passes the redactor;
  talks to: agent, session, config (settings). (v1; reworked v2; extended v3)
- **e2e**: offline tests vs `providertest` in temp dirs/HOME: `Init`, `Compact`, `Undo`, `Secret`
  + v3 `Session` (round-trip fidelity), `Question`, `Retry`; talks to: agent, tui. (v1; v3: +3)

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
Non-blocking; tasks touching an item get opus and negative-case verification. (v3 supersedes the
Scope deferrals "session persistence" and "config writes".)
- C: Transport allowlist = exact `host:port` of configured baseURLs, `Proxy: nil`, redirects
  refused; no other `http.Client`/`http.DefaultTransport` anywhere. PathGuard: `EvalSymlinks` of
  the deepest existing ancestor; outside paths → error results; the v3 session Store routes every
  path through it (`.yipyip` symlinked outside → refused).
- C: Secrets: resolved `{env:}` values → `[redacted]` in UI/errors/tool results/HTTP bodies,
  stripped from the `bash` child env, in no file yipyip writes — session files and settings
  writes included (`SECRET123` negatives). No logs; disk writes: tool-caused + session files
  (`<startdir>/.yipyip/`, `.gitignore`d; save never runs a command) + settings (three keys).
- C: Approvals: "always" per tool per session, reset by `/new`; the gate is the only path to
  `Registry.Run` for gated tools; the v3 `question` tool is never gated (asks, not acts). Undo
  snapshots in memory only; `bash` unsandboxed, process group killed on timeout/Esc.
- C: Public interface (renaming later is breaking): config schema, tool names/args (v3 adds
  `question`), slash commands (v3 adds `/sessions`), session file `version` 1, `<id>` = 12 hex
  chars = 3 bytes of SHA-256 of the file content after the first save.
- C (v3): session files persist tool results un-redacted (the model must see the same on resume);
  interrupted turns stored as finished (their tool calls may have written files); queue → draft
  on resume; live turn markers not persisted or restored.
- C (v3): retries: 10 attempts, waits doubling 1 s→60 s cap (~4 min worst), pre-first-byte
  failures only, same host-restricted transport — `transport.go` untouched (no other host ever
  contacted). Settings write: atomic, key-preserving, `{env:NAME}` literal; failure → one line.
- C (v3): tabs: model/effort/mode are one global selection; `Ctrl+W` saves then moves to the left
  neighbour (else right); last-tab close = `/exit`; 8-tab limit.
- C (v2): start mode `auto` (label + hint never dropped); `atotto/clipboard` (clipboard.go only)
  is the sole exec besides `bash.go`; history/pastes/queue memory-only except the v3 session file.
- Live systems: only `proxy.golang.org` during the build; OpenRouter only when the user runs
  `yip` by hand with a `:free` model; a real host in tests or Makefile fails R18. Deps: no new
  modules in v3; servers omitting usage → chars/4 estimate, never a crash.
- Testability: no teatest; TUI tests drive `Model.Update` and assert on `View()`.
