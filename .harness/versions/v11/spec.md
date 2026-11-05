# v11 spec: headless mode + paste display fix

## 1. Goal
Let another agent (e.g. Claude Code) drive yip without a terminal, so it can run and inspect a
supervisor/worker loop (`/loop-coding vN`, see `sb/human/02_reusables/ai/r_yip.md`) as a repeatable test:
one call = one turn, a full machine-readable event trace, exit codes, hard limits, isolation. Also fix
that a large paste is shown as `[Pasted text #…]` in the chat history after Enter.

## 2. New requirements
- R86: `yip -p <prompt>` / `--prompt <prompt>` runs one turn without the TUI (no tty needed, no Bubble Tea)
  and exits after the turn ended. `-p -` reads the prompt from stdin. Empty prompt → usage error, exit 2.
- R87: A prompt starting with `/` runs like in the TUI: a skill (`/<name> args`) starts a turn with the
  skill; other slash commands that need the UI (`/model`, `/agents`, `/help`...) are refused with exit 2 and one stderr line.
- R88: Output without `--json`: the final assistant text on stdout (plus a newline); progress lines
  (`tool <name>`, `subagent <id> <state>`) on stderr. Secrets are redacted everywhere (existing redactor).
- R89: `--json` prints JSONL on stdout, one object per line, `"type"` first: `text`, `tool_call`
  (agent id, name, redacted args), `tool_result` (agent id, name, ok, truncated output), `subagent`
  (id, state, task, model), `question`, `approval` (tool, summary, decision), `retry`, `error`, then
  exactly one last line `result`: `session_id`, `exit_code`, `reason` (`done|error|question|timeout|max_calls|usage`),
  `final_text`, `model_calls`, `subagents`, `tokens` (in/out), `cost` (when prices are configured),
  `duration_ms`, `tools` (name → count), `files_changed` (paths). Nothing else is written to stdout.
- R90: Exit codes: 0 done, 1 run error (config, provider, session), 2 usage error or crash, 3 a `question`
  was asked and not answered by `--answer`, 4 `--timeout` hit, 5 `--max-model-calls` hit. On 3, 4 and 5 the
  session is saved (unless `--no-save`) and `result.session_id` allows `--resume`.
- R91: `--resume <id>` with `-p` continues that session of the start directory (multi-turn through repeated
  calls); an unknown id → exit 1 and `unknown session <id>` on stderr. `--resume` without id is a usage error with `-p`.
- R92: `--answer <text>` (repeatable): the n-th `question` call of the run gets the n-th answer (an option
  label or free text); a question beyond the given answers ends the run with exit 3 (R90).
- R93: `--approve <tool,...|all>`: in `manual` mode these tools run without a prompt; every other gated call
  is denied (the model gets the usual denial result) and appears as an `approval` event with `decision: denied`.
  Default mode with `-p` is `auto` (the user's saved mode is ignored), so no flag is needed there.
- R94: `--dir <path>` sets the start directory (the only write area) instead of the current one; a missing
  directory → exit 1. `--model <id>`, `--effort <level>`, `--mode auto|manual` override the config for this
  run only and never write `config.json`; an unknown value → exit 2.
- R95: `--timeout <duration>` (Go duration, default none) cancels the turn and its subagents when exceeded;
  `--max-model-calls <n>` counts model calls of the main agent and all subagents and stops the run after n.
- R96: `--system-append <text>` appends text to the system prompt for this run only.
- R97: `--no-save` writes nothing to `.yipyip/sessions` (so `session_id` is empty and R91 cannot resume it);
  `--transcript <file>` writes the full redacted transcript (JSONL as in R89, tool output untruncated) to a
  file inside the start directory; a path outside it → exit 2.
- R98: Headless runs obey all existing security rules (host-restricted network, write confinement, redaction,
  crash log with exit code 2); `/agents`-style subagents of the loop run and are reported via `subagent` events.
- R99: Paste display: submitting an input line (Enter, also a queued line) shows the full pasted text in the
  transcript block where the line had `[Pasted text #N …]`; the input field keeps showing the placeholder
  while typing; the text sent to the model is unchanged (already expanded).
- R100: `README.md` and `/help` text document headless mode (usage, every flag above, exit codes, a short
  example of testing a loop run with `--json`).

## 3. Changed requirements
- R85 and earlier CLI rules: `yip` flags other than `--resume` were rejected as unknown → now the headless
  flags above are accepted; without `-p` they are a usage error (exit 2). `--version`/`--help` unchanged.

## 4. Removed requirements
none

## 5. Constraints & environment changes
none (stdlib only; no new packages, tools, secrets or live systems; tests use the `providertest` fake provider).

## 6. Acceptance criteria
- [ ] [R86,R88] `go test ./e2e/... -run Headless` : `-p` and `-p -` print the final text, exit 0, no tty.
- [ ] [R87] a skill prompt runs the skill; `-p /model` exits 2 with one stderr line.
- [ ] [R89] with `--json` every stdout line parses as JSON, the last is `result` with all listed fields, and tool/subagent events appear for a fake-provider run that spawns a subagent.
- [ ] [R90] tests for exit codes 0, 1, 2, 3, 4, 5 (fake provider: question, hang, endless tool calls).
- [ ] [R91,R97] a second call with `--resume <session_id>` sees the first turn; `--no-save` leaves `.yipyip/sessions` unchanged; `--transcript` outside the start directory exits 2.
- [ ] [R92,R93] `--answer` order test; manual mode denies an unapproved `bash` and runs one named in `--approve`.
- [ ] [R94,R95,R96] `--dir`, overrides leave `config.json` byte-identical, `--system-append` reaches the fake provider request, `--max-model-calls 2` stops after 2 calls (all agents counted).
- [ ] [R98] a secret from the config never appears in stdout, stderr or the transcript file.
- [ ] [R99] TUI test: paste of 3 lines + Enter → transcript contains all 3 lines and no `[Pasted text`; the fake provider received the same text.
- [ ] [R100] `grep -c -- '--max-model-calls' README.md` ≥ 1 and `yip --help` lists `-p`, `--json`, `--answer`.
- [ ] [R86-R100] `make check` still exits 0 (earlier versions keep working).

## 7. Out of scope
Long-running stdio protocol (`--headless` JSON commands), a server/HTTP mode, MCP, sending images, TUI
changes besides R99, changing the loop skill itself.
