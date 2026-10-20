# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

yipyip is a terminal UI coding assistant written in Go (Bubble Tea), in the style of Claude Code and opencode. It talks to OpenAI-compatible chat APIs, edits files and runs shell commands in its start directory, can spawn subagents, and runs in a manual (approval) or auto mode. Its network traffic is restricted to the configured provider hosts. The spec is `README.md`.

## Commands

Go 1.27, GNU Make; the module is `yipyip`. Tests run offline against an `httptest` fake provider.

- Install deps: `go mod download`
- Test: `go test ./...`
- Lint: `gofmt -l .` (must print nothing) and `go vet ./...`
- Build: `go build ./...` (or `make build`)
- Install the binary as `yip` into `$(go env GOBIN)` or `$(go env GOPATH)/bin`: `make install`
- Combined check, exits 0 only if healthy: `make check` (runs `gofmt -l .` with empty output, `go vet ./...`, `go test ./...`, `go build ./...`)

## Architecture

Layout (assumption, from the spec's Tech stack):

- `cmd/yip`: main, flags (`--version`, `--help`), start-directory handling
- `internal/config`: loads `~/.config/yipyip/config.json` plus `yipyip.json` override, `{env:NAME}` substitution
- `internal/provider`: OpenAI-compatible streaming client, own SSE parser, host-restricted HTTP transport
- `internal/agent`: agent loop, modes and approvals, subagents, compaction, undo
- `internal/tools`: `read_file`, `write_file`, `edit_file`, `glob`, `grep`, `bash`, confined to the start directory
- `internal/tui`: Bubble Tea model, slash commands, status line, subagent panel
- `e2e`: end-to-end tests (`/init`, `/compact`, `/undo`) against the fake provider

## Delegation workflow: supervise, don't implement

For non-trivial implementation work, use the supervisor/worker loop (`/loop-coding`, full procedure in `.claude/skills/loop-coding/SKILL.md`) instead of implementing directly:

- Project flow, per version `vN`: spec (`README.md` for v1, `versions/vN/spec.md` for later versions, written with `prompts/new-version.md`) → `prompts/generate-plan.md` → `versions/vN/plan.md` (+ the core `Plan.md`) → `prompts/generate-tasks.md` → `versions/vN/Tasks.md` (small index) + `versions/vN/tasks/<ID>.md` (brief) + `scripts/verify/vN/<ID>.sh` (verification) → loop runs the index row by row and reads a task file only when it dispatches that task; it never reads the folders of other versions.
- Done = a verification script that exits 0, never "looks right"; workers never touch the spec, prompts, skill or this section (the supervisor checks the diff); keep the loop alive with `/goal`.
- One fresh worker per task, `model` per task (haiku / sonnet / opus); the supervisor verifies and commits itself and never trusts the worker's report.
- Fail → concrete feedback to the same worker; still failing after 3 rounds → `blocked` and ask the user.
