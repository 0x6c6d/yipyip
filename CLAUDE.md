# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

yipyip is a terminal UI coding assistant written in Go (Bubble Tea), in the style of Claude Code and opencode. It talks to OpenAI-compatible chat APIs, edits files and runs shell commands in its start directory, can spawn subagents, and runs in a manual (approval) or auto mode. Its network traffic is restricted to the configured provider hosts. The spec is `README.md`.

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
- `internal/config`: `config.go` loads `~/.config/yipyip/config.json` merged key by key with `yipyip.json`; `env.go` resolves `{env:NAME}`; `redact.go` holds every resolved secret value for redaction
- `internal/provider`: `client.go` OpenAI-compatible streaming client; `sse.go` own SSE parser; `transport.go` host-restricted HTTP transport (the only place that builds HTTP clients); `providertest/` httptest fake provider used by tests
- `internal/agent`: `session.go` session state and modes; `loop.go` agent loop; `approval.go` the approval gate shared by main agent and subagents; `subagent.go` subagent manager; `compact.go` compaction; `undo.go` per-turn file journal; `prompt.go` system prompt; `init.go` `/init`
- `internal/tools`: `registry.go` tool registry; `files.go` `read_file`, `write_file`, `edit_file`; `search.go` `glob` (own `**`) and `grep`; `bash.go` `bash` (the only `os/exec` importer); `pathguard.go` start-directory confinement for all file paths
- `internal/tui`: `model.go` Bubble Tea model and keys; `commands.go` slash-command dispatcher; `help.go` `/help` text; `status.go` status line; `agents.go` subagent panel; `approval.go` approval prompt; `blocks.go` and `highlight.go` conversation rendering; `turn.go` turn clock, `Thinking…` line and finished marker; `subagents.go` live subagent blocks; `input.go` suggestions, history, paste placeholders and the queue; `mouse.go` wheel scrolling and copy-on-select; `clipboard.go` clipboard writer (OSC 52 + `atotto/clipboard`, the only exec besides `bash.go`: fixed `wl-copy`/`xclip`/`xsel`, text via stdin, only redacted view text)
- `e2e`: end-to-end tests (`/init`, `/compact`, `/undo`, CLI, secrets) against the fake provider

Security-critical logic lives only in `internal/provider/transport.go`, `internal/tools/pathguard.go`, `internal/tools/bash.go`, `internal/agent/approval.go` and `internal/config/redact.go`; other code calls these and never re-implements them. Non-test code never uses `http.DefaultClient`, `http.DefaultTransport` or `http.Get`/`Post`/`Head`.

## Delegation workflow: supervise, don't implement

For non-trivial implementation work, use the supervisor/worker loop (`/loop-coding`, full procedure in `.claude/skills/loop-coding/SKILL.md`) instead of implementing directly:

- Project flow, per version `vN`: spec (`README.md` for v1, `versions/vN/spec.md` for later versions, written with `prompts/new-version.md`) → `prompts/generate-plan.md` → `versions/vN/plan.md` (+ the core `Plan.md`) → `prompts/generate-tasks.md` → `versions/vN/Tasks.md` (small index) + `versions/vN/tasks/<ID>.md` (brief) + `scripts/verify/vN/<ID>.sh` (verification) → loop runs the index row by row and reads a task file only when it dispatches that task; it never reads the folders of other versions.
- Done = a verification script that exits 0, never "looks right"; workers never touch the spec, prompts, skill or this section (the supervisor checks the diff); keep the loop alive with `/goal`.
- One fresh worker per task, `model` per task (haiku / sonnet / opus); the supervisor verifies and commits itself and never trusts the worker's report.
- Fail → concrete feedback to the same worker; still failing after 3 rounds → `blocked` and ask the user.
