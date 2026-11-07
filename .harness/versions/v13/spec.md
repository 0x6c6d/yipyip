# v13 spec: global AGENTS.md

## 1. Goal

Like `~/.claude/CLAUDE.md`, a user-wide instructions file lets the user give yip standing preferences for every project.
yip reads `~/.config/yipyip/AGENTS.md` (next to `config.json` and `skills/`) and puts it in the system prompt before the project `AGENTS.md`.

## 2. New requirements

- R109. Global file: if `~/.config/yipyip/AGENTS.md` is a regular file (symlinks to a regular file allowed), its content is added to the system prompt of the main agent and of subagents under the heading `# Global AGENTS.md`, placed before the project `# AGENTS.md` section. A missing, unreadable, empty (whitespace only) or non-regular file adds nothing and is no error.
- R110. Limit and redaction: the global content is cut to 30,000 characters (same rule as the project file, invalid UTF-8 replaced) and passes through the secret redactor before it enters the prompt.
- R111. Config dir: the file is looked up in the same directory that holds `config.json` (so a test that sets the config directory, e.g. via `HOME`/`XDG_CONFIG_HOME` as the existing loader does, controls it). Reading it needs no approval and works in headless mode (`yip -p`) too.
- R112. Fresh read: the file is read when a session is created and again when the system prompt is rebuilt (`/new`, `/clear`, `/compact`, as the project file is today); it is not watched.
- R113. `/init` and the `AGENTS.md` writing stay project-only: `/init` never reads or writes the global file, and its prompt does not include the global content.
- R114. `/help` or the README tells where the file lives (see R115).
- R115. README: the repo-root `README.md` documents the global file: path, order (global before project), the 30,000-character limit, and that `/init` does not touch it.

## 3. Changed requirements

- R12: was `/init` writes or updates `AGENTS.md` in the start directory → unchanged, but the system prompt that `/init` runs under now also carries the global section (the `/init` prompt itself embeds only the project file).

## 4. Removed requirements

none

## 5. Constraints & environment changes

none. No new packages, tools, env vars or live systems. The new read is a plain `os.Open` of a file under the config dir; it adds no network and no process start.

## 6. Acceptance criteria

- [ ] Unit test (`internal/agent`): with a global file `GLOBAL-NOTE` and a project `AGENTS.md` `PROJECT-NOTE`, the system prompt contains both, `# Global AGENTS.md` before `# AGENTS.md`; `go test ./internal/agent -run GlobalAgents` exits 0 [R109]
- [ ] Unit test: missing file, empty file, whitespace-only file, directory named `AGENTS.md` each leave the prompt identical to the one without a global file [R109]
- [ ] Unit test: a 40,000-character global file is cut to 30,000 characters; a configured secret value inside it is redacted [R110]
- [ ] e2e test (`go test ./e2e -run GlobalAgents`): with the fake provider, the first request's system message contains the global note, in the TUI and with `yip -p`; the subagent request carries it too [R109, R111]
- [ ] e2e test: after changing the file and running `/new`, the next request carries the new content [R112]
- [ ] e2e test: `/init` request's user prompt does not contain the global note and no file is created or changed in the config dir [R113, R12]
- [ ] `grep -n 'AGENTS.md' internal/tui/help.go README.md` shows the global path in `README.md` and a mention in the help text; README also names the 30,000-character limit [R114, R115]
- [ ] `make check` still exits 0 (earlier versions keep working)

## 7. Out of scope

- Hierarchical lookup of `AGENTS.md` in parent or sub-directories, `@file` imports, `CLAUDE.md` fallback.
- A `/memory` or edit command for the global file; creating the file from yip.
- Merging with a `~/.yipyip/` directory (not created).
- Watching the file for changes during a session.

## Assumptions (confirm)

1. Path is `~/.config/yipyip/AGENTS.md`, not `~/.yipyip/AGENTS.md`, to match `config.json` and `skills/`.
2. Global comes first, project second (project wins on conflict by being later).
3. Same 30,000-character limit separately for each file.
4. Missing file is silent, not an error.
5. Included for subagents too (they reuse the system prompt).
