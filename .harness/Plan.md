# Plan: yipyip

_Core plan, valid for all versions; edited in place by the planner of each version (git is the
history). Phases, Definition of Done and requirement coverage live in `.harness/versions/vN/plan.md`._

## Scope
**Product scope so far:** v1 (README R1–R18): Go/Bubble Tea coding assistant `yip` for
OpenAI-compatible streaming APIs; `{env:NAME}` config, host-restricted HTTP, start-dir-confined
file tools, unrestricted `bash`, agent loop, manual/auto modes, subagents, slash commands,
token-only status line (never cost), `make install` as `yip`. Later: session persistence + config
writes (v3), skills (v4), web tools (v6, v8), markdown (v9), crash log/autosave (v10), v11 headless
one-turn mode `yip -p` (JSONL trace, exit codes, limits) and paste display fix; v12 no leftover processes after
exit (tracker + `KillAll` reaper, Linux subreaper); v13 global `~/.config/yipyip/AGENTS.md` in the system prompt.
**Explicitly deferred (product level):** native Anthropic / OpenAI-Responses protocols, MCP, plugins,
image input, telemetry, auto-update, Windows/macOS, per-subagent model/effort UI, bash sandboxing,
completion/man page, price config, headless stdio protocol / server mode, surviving background jobs
(detach, `/jobs`), non-Linux subreaper, hierarchical/`@file`/`CLAUDE.md` instruction lookup.
**Where the code lives / protected paths (no worker may touch):** code in the repo root
`/home/sa/workspace/code/yipyip` (module `yipyip`): `cmd/yip/`, `internal/{config,provider,agent,
session,skills,tools,tui,crashlog}/`, `internal/provider/providertest/` (shared fake provider), `e2e/`,
`Makefile`, `go.mod`, `go.sum`, `config.example.json`, `.gitignore`. Protected: the whole `.harness/`
folder, `.claude/`, the "Delegation workflow" section of `CLAUDE.md` (the rest may be updated to match the
code). `README.md` is written only by a version's README task (v11: README rewritten, old "sections 1–9
frozen" rule dropped). No worker commits, runs `git init`, or contacts any host except
`proxy.golang.org` (via `go mod download`/`go get`).

## Architecture
Flow: `cmd/yip` → `config.Load` → `tui.New(session)` (or v11 `runHeadless`) → `agent.Session.Submit` →
`provider` SSE events → approval gate → `tools.Registry.Run` → results into history → repeat; the UI
(TUI or headless runner) consumes `NextEvent`/`PollEvents`.
- **cmd/yip**: `--version` (`yip <version>`, default `dev`), `--help` before config load; start dir =
  symlink-resolved `os.Getwd()`; startup errors → stderr + exit 1; v3 `--resume [id]`/`-c` (id reopens, no
  id + TTY → picker, else fresh, unknown → `unknown session <id>` + exit 1); v10 fatal panic →
  `crashlog.Fatal`, `yip: crashed, details in <path>`, exit 2; startup `Store.Prune(25, resumed id)`;
  memory watchdog `crashlog.Watch`; v12 `run` calls `tools.InitSubreaper()` first and, as its first defer,
  `tools.KillAll(2s)` (after save and crash record); TUI SIGTERM/SIGHUP → `tui.SignalQuitMsg` (save, quit). v11 `headless.go`: `-p/--prompt` (`-` = stdin) + `--json`, `--answer`,
  `--approve`, `--dir`, `--model/--effort/--mode`, `--timeout`, `--max-model-calls`, `--system-append`,
  `--no-save`, `--transcript`, `--resume <id>`; no Bubble Tea program; default mode `auto`; never writes
  `config.json`; exit 0 done/1 error/2 usage+crash/3 question/4 timeout/5 max calls; headless flags without
  `-p` → exit 2; talks to: config, agent, session, skills, tools (PathGuard), tui (help text), crashlog.
- **internal/config**: `~/.config/yipyip/config.json` (missing = empty) + `<startdir>/yipyip.json`
  deep-merged key by key, `{env:NAME}` (unset/empty → error naming `NAME`, exit 1), provider/model
  parsing, `max_subagents` (4), `auto_compact` (true), ignores `cost`/`npm`; **Redactor** (resolved
  secrets → `[redacted]`); v3 settings write (only `model`/`effort`/`mode`, atomic, `{env:}` literal);
  v6 `search` (`baseURL` default `https://api.search.brave.com`, `apiKey` required, https unless loopback);
  v8 `jev` key ignored; v13 `Config.GlobalDir` (= dir of `config.json`, set by `Load`); talks to: nothing.
- **internal/provider**: `Client.Stream(ctx, req)` → `POST <baseURL>/chat/completions` (Bearer if set,
  `stream`, `include_usage`, `tools`, `reasoning_effort` unless `reasoning:false`); own SSE parser;
  errors → redacted `ErrorEvent`s; v3 retry of pre-first-byte failures (dial/TLS/timeout, reset, 429, 5xx):
  ≤10 attempts, waits 1,2,4,…,60 s, `retrying (n/10) in <wait>`; **Transport**: the single RoundTripper,
  per-client `host:port` allowlist, `Proxy: nil`, foreign hosts/redirects rejected; v6
  `NewSearchHTTPClient` (search host only, 20 s) + Brave client; v8 `NewFetchHTTPClient` (`web_fetch`
  only): any host, dial-time IP check (no loopback/private/link-local/unspecified/multicast), ≤5
  re-checked redirects, 20 s, no jar/auth, `User-Agent: yipyip`; `providertest` scripted SSE fake (failures,
  `Delay`, `NewFunc`, Brave); talks to: config.
- **internal/skills** (v4): `Load` from `~/.config/yipyip/skills/` + `<startdir>/.yipyip/skills/` (project
  wins, PathGuard), `<name>/SKILL.md`, `key: value` frontmatter, name `^[a-z0-9-]+$`, bad entries skipped with
  a warning; read-only; talks to: tools.
- **internal/session** (v3): `Store` at `<startdir>/.yipyip/sessions`, `SessionData`, `Save`/`Load`/`List`
  (newest first)/`Delete`, atomic temp+rename, PathGuard paths, `version`≠1/invalid JSON → error naming the
  file; v10 `Prune(keep, protect...)` (protected ids and non-`<12 hex>.json` never deleted); v11
  `FromAgent`/`ApplyTo` helpers shared by TUI and headless; imports agent only.
- **internal/tools**: `Registry` (`read_file`, `write_file`, `edit_file`, `glob`, `grep`, `bash`; v6
  `web_search` only with `search`; v8 `web_fetch` always, gated, stdlib HTML→text, 1 MiB, 20,000 chars,
  untrusted marker; no other web-like name), **PathGuard** (`EvalSymlinks` of the deepest existing ancestor;
  `Resolve` write mode confined; v6 `ResolveRead` anywhere for `read_file`/`glob`/`grep`), **Bash runner**
  (only `os/exec` site, env without `{env:}` vars), undo pre-write hook; v12 **Reaper** (`reaper_linux.go`):
  process-wide tracker of `BashRunner` pgids, `InitSubreaper`, `KillAll(grace)` (tracked live groups + `/proc`
  escapees, TERM → poll → KILL, zombie reap by pid); talks to: config, provider.
- **internal/agent**: `Session` (history, model/effort/mode, counters, turns, export/restore); loop (v7 main
  turn unlimited; v5 ≤500 per subagent run; per-turn ctx, cancel); **Approval gate** (`y/n/a`, "always" per
  tool per session; gated incl. `web_search`, `web_fetch`); v3 **Question gate** (`question` never gated,
  blocks on a reply channel); **Subagent manager**, **Undo journal**, compaction, `/init`, system prompt
  (+`AGENTS.md`; v13 `# Global AGENTS.md` from `Config.GlobalDir` first, same read/cap/redaction, not in `/init`);
  v10 checkpoint after each complete main round + `EvCheckpoint`, recovered panics →
  `crashlog.Recovered`; v11 `Options.SystemAppend`, `Options.MaxModelCalls` (one counter for main +
  subagents, `ErrModelCallLimit`), `ModelCalls()`; v12 `Shutdown()` (cancel turn + subagents); talks to: provider, tools.
- **internal/tui**: Bubble Tea `Model` (alt screen, mouse): v3 tabs (≤8, per-tab conversation/counters/
  history/pastes/queue/turn; global model/effort/mode); v4 one-row tab bar, `×`/`+`, only `Ctrl+T`/`Ctrl+W`;
  viewport (chroma, subagent blocks), textarea (suggest, history, pastes, queue, click-to-cursor, select-to-
  copy), status rows, slash commands (+`/sessions`, `/skills`, `/<skill>`, built-ins win); save of every
  tab on exit; overlays (model picker, approval prompt with query/URL, resume picker); inline question
  block; settings persist on change; alt+enter newline; text passes the redactor; v9 `markdown.go`, `resetTab`;
  v10 autosave (turn end + `EvCheckpoint`, ≥1 min apart, then `Prune`), panic guard, per-block `[render
  error]`; v11 submitted/queued lines show pastes expanded, `/help` documents headless; talks to: agent,
  session, config, skills, crashlog.
- **internal/crashlog** (v10): stdlib only; `<startdir>/.yipyip/crash.log` (dir 0700, file 0600) via injected
  `Resolve`; records (time, version, panic, stack, session id) redacted by an injected func; 1 MiB → 512 KiB;
  `Fatal`, `Recovered`, `Watch` (memory records, `.yipyip/heap.pprof`), package default; talks to: nothing.
- **e2e**: offline tests vs `providertest` in temp dirs/HOME: `Init`, `Compact`, `Undo`, `Secret`, `CLI`,
  `Session`, `Question`, `Retry`, `Skills`, `WebFetch`, `Markdown`, `Crash`, `Autosave`, `Retention`, v11
  `Headless` (built binary via `cliBinary`), v12 `Cleanup`, v13 `GlobalAgents`; talks to: agent, tui, cmd/yip binary.

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
if a wrong result costs more to redo. Precedence: a security or Consequential rule beats a cost example.
- **haiku**: clear boundary, small diff, mechanically verifiable. Examples: `config.example.json`,
  `.gitignore`, Makefile targets, `--version`, `/help` text and README sections, v11 R99 `shown` fix.
- **sonnet** (default): multi-file features, moderate design judgment, new patterns. Examples: SSE client
  (on the opus transport), `glob`/`grep`/`read_file`, TUI views, slash dispatch, `/compact`, `/init`, e2e
  tests, v11 flag parsing and JSONL event mapping.
- **opus**: architecture, security-sensitive code (auth, secrets, crypto, parsing untrusted input,
  shell/SQL/command construction), concurrency, cross-cutting refactors, anything touching a Consequential
  assumption. Examples: Transport; PathGuard; Bash runner; approval gate; Redactor; subagent manager, turn
  cancellation; undo journal; v8 fetch client; v9 `markdown.go`, `resetTab`; v10 `crashlog`, `Prune`,
  autosave, checkpoints; v12 reaper + exit-path wiring; v11 headless runner (approval/question replies, timeout/cancel, transcript path,
  redaction) and the shared model-call counter.
- **Shared-code rule:** security-critical logic lives only in shared, opus-built code:
  `internal/provider/transport.go`, `internal/tools/pathguard.go`, `internal/tools/bash.go` (sole `os/exec`
  site), v12 `internal/tools/reaper*.go` (sole signal site; others call `tools.KillAll`), `internal/agent/approval.go`, `internal/config/redact.go`, `internal/crashlog/`. Callers (file tools,
  client, TUI, headless output) may be sonnet; the supervisor runs the negative checks itself
  (`..`/symlink escape, foreign host, unprompted bash, `SECRET123`).

## Risks & Open Questions
Non-blocking; tasks touching an item get opus and negative-case verification.
- C: Transport allowlist = exact `host:port` per client (providers; v6 search host, settable by project
  `yipyip.json`); `Proxy: nil`, no foreign redirects. PathGuard `Resolve` (writes, undo, Store, skills,
  crash log, v11 `--transcript`) confined; `ResolveRead` reads anywhere, ungated, redacted (`~/.ssh` readable).
- C: Secrets (every `{env:}` value, `BRAVE_API_KEY`/`search.apiKey`) → `[redacted]` in UI/errors/tool results/
  HTTP bodies (skill text, search query/results, fetch URL), v11 headless stdout/stderr/transcript; stripped
  from `bash` env; in no written file except un-redacted tool results in session files (v3).
- C: Approvals: "always" per tool per session, reset by `/new`/`/clear`; the gate is the only path to
  `Registry.Run` for gated tools; `question` never gated; skills grant nothing; v11 headless answers the gate
  from `--approve` only (unknown name → exit 2), everything else denied. `bash` unsandboxed, process group
  killed on timeout/Esc. Public interface: config schema, tool names/args, slash commands, `<name>/SKILL.md`,
  session `version` 1, `<id>`, v11 CLI flags, JSONL record shapes and exit codes 0–5.
- C (v3/v4/v9): interrupted turns stored as finished; queue → draft on resume; retries 10× pre-first-byte;
  settings write atomic; one global model/effort/mode; tab close saves then moves left; last-tab close →
  fresh empty tab, no save, no quit; 8 tabs; v4 drops `Ctrl+Tab`/`Alt+N`/Shift+Enter; a markdown render that
  un-splits a secret falls back to the redacted source.
- C (money): v5 subagent ≤500 model calls (× `max_subagents`); v7 main agent unlimited, `Esc` the only TUI
  brake; v11 headless `--timeout`/`--max-model-calls` (counts main + subagents, (n+1)-th request refused)
  are the only automated brakes. v2: start mode `auto`; `atotto/clipboard` the sole exec besides `bash.go`.
- C (v8): `web_fetch` = the only client to arbitrary hosts (SSRF): IP checked at dial after `Unmap`, test-only
  `AllowAddr`, URL redacted, gated, content marked untrusted.
- C (v10): `crash.log` holds redacted panic values/stacks and memory records only; checkpoints replace history
  mid-turn (main agent, complete rounds); autosave rewrites one id while running; `Prune` deletes session
  files (newest 25 kept, open tabs / resumed id protected, stray names untouched).
- C (v11): headless `--dir` is the write area (no chdir); saves once at run end (not on crash), keeps a
  resumed id, then `Prune(25, id)`; `--no-save` writes nothing under `.yipyip/sessions`; overrides never
  touch `config.json`; saved mode ignored with `-p`.
- C (v12): exit kills every live member of a tracked `bash` group and every yip descendant in a tracked group,
  below one, or in another session (setsid escapees, via `PR_SET_CHILD_SUBREAPER`); untracked same-session
  children (clipboard helpers) survive; signalled only after `kill(-pgid,0)` + non-zombie `/proc` member;
  zombies reaped by pid only; exit delay ≤2.5 s; background servers never survive yip.
- C (v13): `<GlobalDir>/AGENTS.md` (plain `os.Open`, no PathGuard, regular file only) is sent, redacted and cut to
  30,000 chars, in every main/subagent system prompt; read at session create and `Reset` only; yip never writes it.
- Historical verify notes: v5 `T401.sh`/`T799.sh` rewritten for v7; v4–v7 scripts pin jev and are not rerun;
  diff guards use v8 start `a99a82f`, v10 start `1ccbbc7`.
- Live systems: only `proxy.golang.org` during the build; OpenRouter/Brave/`web_fetch` URLs only when the
  user runs `yip` by hand; a real host in tests or Makefile fails R18. No new modules; missing usage →
  chars/4 estimate. Testability: no teatest; TUI tests drive `Model.Update` + `View()`; headless e2e runs
  the built binary against `providertest`.
