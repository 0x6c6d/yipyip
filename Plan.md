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
- **cmd/yip**: `--version` (`yip <version>`, default `dev`), `--help` (usage, slash commands, keys)
  before config load; start dir = symlink-resolved `os.Getwd()`; startup errors → stderr + exit 1;
  v3 `--resume [id]`/`-c`: id reopens, no id + TTY → picker, non-TTY/empty → fresh, unknown id →
  `unknown session <id>` + exit 1; v4: no modifyOtherKeys request; talks to: config, agent,
  session, tui. (v1; v2; v3; v4)
- **internal/config**: `~/.config/yipyip/config.json` (missing = empty) + `<startdir>/yipyip.json`
  deep-merged key by key, `{env:NAME}` (unset/empty → error naming `NAME`, exit 1), provider/model
  parsing, `max_subagents` (4), `auto_compact` (true), ignores `cost`/`npm`; **Redactor** (resolved
  secrets → `[redacted]`); v3 settings write (only `model`/`effort`/`mode`, atomic, `{env:}` kept
  literal, never a secret); v4/v5 `jev` (`baseURL` `https://api.typesafe.ai`, `apiKey`, `model`
  `jev-latest`, `min_confidence` 0.7, `route.easy/medium/hard` = configured models, only `hard`
  required); v6 `search` (`baseURL` `https://api.search.brave.com`, `apiKey` required, https unless
  loopback, errors name the key); talks to: nothing. (v1; v3; v4; v5; v6)
- **internal/provider**: `Client.Stream(ctx, req) <-chan Event` → `POST <baseURL>/chat/completions`
  (Bearer if set, `stream:true`, `include_usage:true`, `tools`, `reasoning_effort` unless
  `reasoning:false`); own SSE parser; 4xx/5xx/dropped streams → redacted `ErrorEvent`s; v3 retry:
  pre-first-byte failures (dial/TLS/write timeout, reset, 429, 5xx) → ≤10 attempts, waits
  1,2,4,8,16,32,60,60,60 s, `retrying (n/10) in <wait>` event; **Transport**: the single
  `http.RoundTripper` type, per-client `host:port` allowlist (providers + v4 `jev.baseURL` while
  set; v6 `NewSearchHTTPClient`: only the search host, 20 s), `Proxy: nil`, other hosts + redirects
  rejected; v4 **JevClient** `POST <baseURL>/v1/systemone` (Bearer, `{model,state,questions}`);
  v6 `search.go` Brave client `GET /res/v1/web/search`; `providertest`: scripted SSE fake (v3
  failures; v4 System One; v6 Brave); talks to: config. (v1; v3: retry; v4: jev; v6: search)
- **internal/skills** (v4): `Load` from `~/.config/yipyip/skills/` + `<startdir>/.yipyip/skills/`
  (project wins, project paths via PathGuard), `<name>/SKILL.md`, hand-parsed `key: value`
  frontmatter, name `^[a-z0-9-]+$`, bad entries skipped with one warning; read-only; talks to: tools.
- **internal/session** (v3): `Store` at `<startdir>/.yipyip/sessions`, `SessionData` (R29 fields),
  `Save`/`Load`/`List` (newest first)/`Delete`; atomic temp+`os.Rename`; every path via PathGuard;
  `version`≠1 or invalid JSON → error naming the file; unknown fields ignored; imports agent only.
- **internal/tools**: `Registry` (`read_file`, `write_file`, `edit_file`, `glob`, `grep`, `bash`;
  v6 `web_search` only with `search`, no other web-like name), **PathGuard** (`EvalSymlinks` of the
  deepest existing ancestor; `Resolve` = write mode, rejects outside; v6 `ResolveRead` for
  `read_file`/`glob`/`grep`, anywhere), **Bash runner** (only `os/exec` site, env without `{env:}`
  vars), undo pre-write hook; talks to: config (redactor), provider (search). (v1; v6)
- **internal/agent**: `Session` (history, model/effort/mode, counters, turns, v3 export/restore);
  loop (≤50 model calls per main turn, v5 ≤500 per subagent run, per-turn ctx, Esc cancels);
  **Approval gate** (`y/n/a`, "always" per tool per session; v5 no jev hook; v6 + `web_search`);
  v3 **Question gate**: `question` (never gated, v5 always to the user) blocks on a TUI reply
  channel → labels joined `, ` / free text / `cancelled`; **Subagent manager**, **Undo journal**,
  compaction, `/init`, system prompt (+`AGENTS.md`); v4 `jev.go` **Jev decider** (v5: per-turn
  routing only, score 0/1/2 → easy/medium/hard, 10 s timeout/error/low confidence → session
  model, in-memory per-tier stats; v4 question auto-answer and risk screen removed); talks to:
  provider, tools. (v1; v3: question, restore; v4: jev; v5: routing only)
- **internal/tui**: Bubble Tea `Model` (alt screen, mouse): v3 **tabs** (≤8, per-tab conversation/
  counters/history/pastes/queue/turn, global model/effort/mode); v4 bar exactly 1 row (`…`,
  current tab always visible), clickable `×`/`+`, tab keys only `Ctrl+T`/`Ctrl+W`; viewport
  (chroma, subagent blocks), textarea (suggest, history, pastes, queue; v4 click-to-cursor, v5
  fixed: geometry from the rendered layout, textarea's own cursor; select-to-copy), status rows
  (v4 `jev`, routed model), scroll, copy-on-select; slash commands + v3 `/sessions` + v4
  `/skills`, `/jev` (v5 prints the route; `/jev stats` per tier), `/<skill>` (built-ins win);
  save of every tab on exit; overlays: model picker, approval prompt (v6 `web_search`: the query),
  resume picker; v4 question block inline in the
  transcript; settings persist on change; only alt+enter newline (v4); text passes the redactor;
  talks to: agent, session, config, skills. (v1; reworked v2; extended v3, v4)
- **e2e**: offline tests vs `providertest` in temp dirs/HOME: `Init`, `Compact`, `Undo`, `Secret`,
  v3 `Session`, `Question`, `Retry`, v4 `Skills`, `Jev`; talks to: agent, tui. (v1; v3; v4)

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
deferrals "session persistence"/"config writes"; v4 skills "custom slash commands"; v6 "web tools".)
- C: Transport allowlist = exact `host:port` per client: providers (+ v4 `jev.baseURL` while set),
  v6 search client only `search.baseURL` (project `yipyip.json` may set it, like providers);
  `Proxy: nil`, no foreign redirects, no other clients. PathGuard `Resolve` (writes, undo, Store,
  skills) confined; v6 `ResolveRead` reads anywhere, ungated, redacted (`~/.ssh` readable).
- C: Secrets (v4 `TYPESAFE_API_KEY`, v6 `BRAVE_API_KEY`/literal `search.apiKey`) → `[redacted]` in
  UI/errors/tool results/HTTP bodies (jev `state`, skill text, v6 `web_search` query + results),
  stripped from `bash` env, in no written file (`SECRET123`). No logs; writes: tools, `.yipyip/`, settings.
- C: Approvals: "always" per tool per session, reset by `/new`; the gate is the only path to
  `Registry.Run` for gated tools (v6 + `web_search`); `question` never gated; skills grant nothing;
  v5: jev never answers a `question` nor touches the gate. Undo in memory; `bash` unsandboxed,
  process group killed on timeout/Esc. Public interface: config schema (v4 `jev`, v6 `search`),
  tool names/args (v6 `web_search`), slash commands, `<name>/SKILL.md`, session `version` 1, `<id>`.
- C (v3): session files keep tool results un-redacted; interrupted turns stored as finished; queue
  → draft on resume. Retries: 10 attempts, 1→60 s, pre-first-byte only. Settings write atomic.
- C (v3/v4): tabs: one global model/effort/mode; `Ctrl+W`/`×` save then move left (else right);
  last-tab close = `/exit`; 8-tab limit; v4 drops `Ctrl+Tab`/`Alt+N`/Shift+Enter (breaking).
- C (v4): jev `state` minimal (≤2000 chars) + redacted; 10 s timeout + fallback; off at start.
  C (v5): jev only routes (state = redacted request); `jev.route` needs only `hard`, absent tiers →
  `hard`, v4 two-key routes still load; subagent runs up to 500 model calls (× `max_subagents`).
  C (v2): start mode `auto`; `atotto/clipboard` (clipboard.go) is the sole exec besides `bash.go`;
  history/pastes/queue memory-only except the v3 session file.
- Live systems: only `proxy.golang.org` during the build; OpenRouter/`api.typesafe.ai`/Brave only when
  the user runs `yip` by hand; a real host in tests or Makefile fails R18. No new modules; missing
  usage → chars/4 estimate. Testability: no teatest; TUI tests drive `Model.Update` + `View()`.
