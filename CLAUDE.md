# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

yipyip is a terminal UI coding assistant written in Go (Bubble Tea), in the style of Claude Code and opencode. It talks to OpenAI-compatible chat APIs, edits files and runs shell commands in its start directory, can spawn subagents, can read one web page with `web_fetch`, loads user skills (`/<name>`), and runs in a manual (approval) or auto mode. Its network traffic is restricted to the configured provider hosts (plus, for `web_search` only, the Brave host while configured, and for an approved `web_fetch` call, the host of its URL). It reads files anywhere but writes only inside the start directory. The spec is `README.md`.

## Commands

Go 1.27, GNU Make; the module is `yipyip`. Tests run offline against the `httptest` fake provider in `internal/provider/providertest`.

- Install deps: `go mod download` (only `proxy.golang.org` is contacted)
- Test all: `go test ./...`; end-to-end only: `go test ./e2e/...`
- Lint: `gofmt -l .` (must print nothing) and `go vet ./...`
- Build: `make build` (also builds `bin/yip`, with the version from git)
- Install the binary as `yip` into `$(go env GOBIN)` or `$(go env GOPATH)/bin`: `make install`
- Combined check, exits 0 only if healthy: `make check` (`gofmt -l .` must be empty, `go vet ./...`, `go test ./...`, `go build ./...`)

## Architecture

- `cmd/yip`: `main`, `--version` / `--help`, start-directory handling, runs the Bubble Tea program
- `internal/config`: `config.go` loads `~/.config/yipyip/config.json` merged key by key with `yipyip.json`; `env.go` resolves `{env:NAME}`; `search.go` the optional `search` block (Brave `baseURL`, `apiKey`); `redact.go` holds every resolved secret value for redaction; `settings.go` writes model/effort/mode
- `internal/session`: `store.go` Store with Save/Load/List/Delete for `SessionData` (`session.go`); atomic writes, confined to `<startdir>/.yipyip/sessions`; id = first 3 bytes of the SHA-256 of the content, as 12 hex chars
- `internal/provider`: `client.go` OpenAI-compatible streaming client, retrying failures before the first SSE byte; `sse.go` own SSE parser; `transport.go` host-restricted HTTP transport (the only place that builds HTTP clients); `NewSearchHTTPClient` is a separate search-only client (only the `search.baseURL` host, 20 s); `NewFetchHTTPClient(FetchOptions)` is a separate `web_fetch`-only client (any host, dial-time IP check refusing loopback/private/link-local/unspecified/multicast addresses, at most 5 re-checked redirects, 20 s, no cookie jar, no `Authorization`, `User-Agent: yipyip`; `ValidateFetchURL`); `search.go` Brave search client; `providertest/` httptest fake provider, fake Brave endpoint (`brave.go`) used by tests
- `internal/agent`: `session.go` session state and modes; `loop.go` agent loop (no model-call limit per main turn, 500 per subagent run); `approval.go` the approval gate shared by main agent and subagents (it also gates `web_fetch`; `agent.Options.FetchClient` is the test seam); `subagent.go` subagent manager; `compact.go` compaction; `undo.go` per-turn file journal; `prompt.go` system prompt; `init.go` `/init`; `question.go` question gate and tool
- `internal/skills`: `skills.go` loader for `<name>/SKILL.md` skill folders in `~/.config/yipyip/skills` and `<startdir>/.yipyip/skills` (project wins)
- `internal/tools`: `registry.go` tool registry; `files.go` `read_file`, `write_file`, `edit_file`; `search.go` `glob` (own `**`) and `grep`; `bash.go` `bash` (the only `os/exec` importer); `websearch.go` gated `web_search` (registered only with a `search` block); `webfetch.go` gated `web_fetch` (always registered; 1 MiB read, 20,000 characters, untrusted-content marker); `htmltext.go` stdlib HTML-to-text converter; the web-like name guard in `registry.go` exempts exactly `web_search` and `web_fetch`; `pathguard.go` path resolution: `Resolve` confines writes to the start directory, `ResolveRead` lets `read_file`/`glob`/`grep` read anywhere
- `internal/tui`: `model.go` Bubble Tea model and keys; `commands.go` slash-command dispatcher; `help.go` `/help` text; `status.go` status line; `agents.go` subagent panel; `approval.go` approval prompt (shows the `web_search` query and the `web_fetch` URL); `blocks.go` and `highlight.go` conversation rendering; `turn.go` turn clock, `Thinking…` line and finished marker; `subagents.go` live subagent blocks; `input.go` suggestions, history, paste placeholders and the queue; `mouse.go` wheel scrolling, copy-on-select and input click/select (`inputmouse.go`); `clipboard.go` clipboard writer (OSC 52 + `atotto/clipboard`, the only exec besides `bash.go`: fixed `wl-copy`/`xclip`/`xsel`, text via stdin, only redacted view text); `resume.go` `--resume` picker and restore, `sessions.go` `/sessions`, `tabs.go` tabs, `tabbar.go` tab bar with `×`/`+` hit zones, `tabclick.go` tab bar clicks, `tabkeys.go` swallows the removed tab hotkeys (only `Ctrl+T`/`Ctrl+W` remain), `question.go` inline question in the transcript, `settings.go` settings saves, `skills.go` `/skills` and skill dispatch, `save.go` session save on exit (tabs: up to 8, per-tab state, global model/effort/mode)
- `e2e`: end-to-end tests (`/init`, `/compact`, `/undo`, CLI, secrets, `Session`, `Question`, `Retry`, `Skill`, `WebFetch`) against the fake provider

Security-critical logic lives only in `internal/provider/transport.go`, `internal/tools/pathguard.go`, `internal/tools/bash.go`, `internal/agent/approval.go` and `internal/config/redact.go`; other code calls these and never re-implements them. Non-test code never uses `http.DefaultClient`, `http.DefaultTransport` or `http.Get`/`Post`/`Head`.

## Delegation workflow: supervise, don't implement

For non-trivial implementation work, use the supervisor/worker loop (`/loop-coding`, full procedure in `.claude/skills/loop-coding/SKILL.md`) instead of implementing directly:

- Project flow, per version `vN`: spec (`README.md` for v1, `versions/vN/spec.md` for later versions, written with `prompts/new-version.md`) → `prompts/generate-plan.md` → `versions/vN/plan.md` (+ the core `Plan.md`) → `prompts/generate-tasks.md` → `versions/vN/Tasks.md` (small index) + `versions/vN/tasks/<ID>.md` (brief) + `scripts/verify/vN/<ID>.sh` (verification) → loop runs the index row by row and reads a task file only when it dispatches that task; it never reads the folders of other versions.
- Done = a verification script that exits 0, never "looks right"; workers never touch the spec, prompts, skill or this section (the supervisor checks the diff); keep the loop alive with `/goal`.
- One fresh worker per task, `model` per task (haiku / sonnet / opus); the supervisor verifies and commits itself and never trusts the worker's report.
- Fail → concrete feedback to the same worker; 2 failed rounds → discard and escalate once to a fresh worker on the next model of `haiku → sonnet → opus` (the row's tier is the base model); still failing after 3 rounds on the last model → `blocked` and ask the user.
