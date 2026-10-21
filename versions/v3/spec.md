# yipyip v3: sessions, tabs, questions — a durable daily driver

## 1. Goal

v3 makes a yipyip session durable and resumable, and turns yipyip into a daily driver:
sessions are saved on exit and reopened with `--resume`; up to 8 sessions run side by side
as tabs in one terminal window; the model can ask the user a question with a selectable
option list (Claude Code style); provider connections retry on timeout; model/mode/effort
persist as defaults for the next start, even after a reboot; shift+enter inserts a newline
where the terminal sends a distinct sequence.

## 2. New requirements

- R29. Session file: a session is stored as JSON in `<startdir>/.yipyip/sessions/<id>.json`
  (assumption: per-start-directory, like `yipyip.json`; `<id>` = 12 hex chars of the SHA-256 of
  the file content after the first save (assumption), first 3 bytes). Fields: `version` (file
  format, always `1`), `id`, `created`, `updated`, `startdir`, `model`, `effort`, `mode`,
  `in_tokens`, `out_tokens`, `total_tokens`, `last_context`, `conversation` (the full
  `agent.Session` history incl. tool calls and results), `history` (input history, oldest first,
  cap 500), `pastes` (`{"n": N, "entries": {"#N": "text"}}`), `queue` (queued messages, order),
  `title` (first 60 runes of the first user message, single line), `turn` (running/last turn
  state: `active`, `started`, `interrupted` — an interrupted turn is stored as finished, because
  its tool calls may have written files (assumption)). Unknown fields are ignored on load; a
  missing file, `version` > 1, or invalid JSON is an error naming the file, never a crash; a
  session with `version` < 1 is rejected.
- R30. Save on exit: `/exit`, `/quit` and double `Ctrl+C` write the file of every open tab
  (unless that tab's conversation is empty: nothing is written, no error), restore the terminal,
  exit 0. The write goes through `internal/tools`' confined writer (start-dir confinement,
  `PathGuard`); the file is written atomically (temp file + rename in the same directory).
  Secrets are never stored: keys live only in env vars, the session file holds no API key
  (R16); `{env:NAME}` references in config are re-resolved at resume, not persisted. A failed
  save prints one line `could not save session: <err>` to that tab's conversation and still
  exits 0 (assumption: exit 0, like v1).
- R30a. Save on exit-path errors: if the program exits via a startup error there is no session
  to save (unchanged v1 behaviour); this requirement adds no new behaviour, it records that
  v1 exit codes are unchanged (assumption).
- R31. `--resume [id]` (alias `-c`): with an id, reopen that session; without, pick
  interactively when stdin is a TTY: `yip --resume` shows the picker of saved sessions (newest
  first, `title`, relative `updated` time (assumption: `2h ago` style), tokens, model;
  `Up`/`Down` move, `Enter` opens, `Esc` exits without starting). A non-TTY or empty sessions
  dir starts a new session (assumption). `yip --resume <bad-id>` exits 1 with
  `unknown session <id>` on stderr (naming no secret). The resumed session keeps id, created,
  conversation, counters, history/pastes/queue (queue becomes the draft input, joined `\n`,
  since no turn is running at resume (assumption)); it opens in a new tab (R38) when there is
  room, else replaces the current tab (assumption); its stored model/effort/mode become the
  global selection (assumption). `/new` on a resumed session starts fresh (new id, new tab per
  R38) and keeps the old file (assumption: nothing is deleted silently); a file removed on disk
  disappears from the picker.
- R32. `/sessions`: lists saved sessions of the start dir (`id`, title, `updated` (local time
  `15:04  Jan 2`), tokens total, model) in the conversation, newest first, at most 10 with
  `… +N more`; `/sessions <id>` loads that session into the current tab (replacing conversation,
  counters, history/pastes, queue → draft, marker lines dropped (assumption: live markers are
  turn state, not conversation content)); `/sessions rm <id>` deletes the file (confirmation
  prompt `y/n`, `n` keeps it; the current session's own file is never listed for rm
  (assumption)); `/sessions` with no sessions prints `no saved sessions`.
- R33. Privacy & security: session files live only under `<startdir>/.yipyip/sessions/`, are
  written only by the save path (never a log file), contain no API keys and no redacted-view
  placeholders; tool results are stored as sent to the model (the un-redacted tool output
  (assumption: the model must see the same content on resume), so a session file can contain
  file contents and command output, exactly like the provider already receives them). `bash`
  receives an env without the config's `{env:}` variables (unchanged) and the save path never
  runs a command. `.yipyip/` is added to the repo's `.gitignore` (assumption). A
  `yip --resume` in a different directory never sees another directory's sessions.
- R34. Round-trip fidelity: after save + resume, the next request to the provider contains the
  same messages as if the session had run uninterrupted (same `system` (incl. `AGENTS.md`), same
  user/assistant/tool messages in order, same `reasoning_effort`, same model) — verified by a
  test that runs the same scripted turn with and without a save/resume in between and diffs the
  recorded requests. Token counters, mode, effort, model and input history are identical after
  resume. Live turn state (`Thinking…` line, finished markers) is not part of the request and
  not restored (assumption).
- R35. Session picker TUI: reuses the model picker's visual pattern (centered list, dim frame,
  title `resume session`); rows `title` (bold first line (assumption)), `updated ago · N toks ·
  provider/model`; max 12 rows visible, then scroll indicator `▲/▼` (assumption). `Enter`
  resumes, `Esc`/`q` exits `yip` without starting a session (exit 0). No mouse needed. No
  teatest: tests drive `Model.Update` and assert on `View()`.
- R35a. `/sessions` list formatting: `id` in color 244, title default, `updated` in color 244,
  tokens and model default, two spaces between columns (assumption).
- R36. Shift+enter newline: `shift+enter` inserts a newline in the input field wherever the
  terminal delivers a distinct sequence for it (assumption: where it does not, alt+enter (v1)
  remains the newline key, and `/help` keeps documenting both). Bound in addition to, never
  instead of, alt+enter.
- R37. Connection retries: a provider request that fails before the first response byte
  (dial/TLS/write timeout, connection reset, HTTP 429 or 5xx status) is retried up to
  **10 attempts total** with waits `1s, 2s, 4s, 8s, 16s, 32s, 60s, 60s, 60s` (doubling from 1s,
  capped at 60s; the cap makes the worst case about 4 minutes). No retry after the first SSE
  byte has arrived (a broken stream is a normal error line, unchanged v1) and no retry on
  other 4xx (bad request, auth). While waiting, the status line shows
  `retrying (n/10) in <wait>` (assumption). Retries use the same host-restricted transport,
  the same request body and never contact any other host; `internal/provider/transport.go`
  is not modified.
- R38. Tabs: one terminal window holds up to 8 sessions as tabs. A tab bar renders as the top
  line: one segment per tab `N:<title>` (title = session title, first 60 runes), the current tab
  highlighted, color 244 for the others (assumption). `Ctrl+T` opens a new tab with a fresh
  session; `Ctrl+W` closes the current tab — its session is saved via the R30 path first, and
  with more than one tab open the program keeps running on the neighbouring tab
  (assumption: left neighbour, else right); closing the last tab is exactly `/exit`.
  `Ctrl+Tab` / `Ctrl+Shift+Tab` cycle tabs, `Alt+1`–`Alt+8` jump to tab N. At the limit of 8,
  `Ctrl+T` prints one line `tab limit reached` to the current tab (assumption). Each tab has
  its own conversation, counters, history, pastes, queue and turn state; model, effort and
  mode are global (one selection shared by all tabs (assumption)); `/new` opens a fresh
  session in a new tab. `v1`'s `Tab` (completion) and `Shift+Tab` (mode toggle) keys are
  unchanged. On exit every open tab's session is saved (R30).
- R39. Question tool: the model can call a new tool `question` (auto-approved, never gated —
  it asks, it does not act) with arguments: `question` (text), `options` (list of up to 6 short
  labels), `multiple` (bool, default false). The TUI renders a centered overlay in the style of
  the approval prompt: the question, one row per option — radio button `○/●` for single
  choice, checkbox `[ ]`/`[x]` for multiple — and an always-available free-text input row
  (`type your own answer`). `↑`/`↓` move, `Space` (de)selects (single choice moves the `●`),
  `Enter` confirms, `Esc` cancels. The tool result is the selected label(s) (multiple: joined
  `, `), or the free text, or `cancelled` on Esc; then the turn continues normally. A turn is
  not marked finished while the overlay waits (assumption: like the approval gate, waiting for
  the user is not an error and interrupts are handled as in v1).
- R40. Settings persistence: whenever model, effort or mode is changed in the TUI (picker,
  toggle, resumed session's stored values), those three keys are written to
  `~/.config/yipyip/config.json` (assumption: user-level defaults, so any later `yip` — after
  exit or reboot, in any directory — starts with the last selection). The write is atomic
  (temp + rename), preserves every other key in the file including unknown ones, and never
  writes or moves secret values (a key stored as `{env:NAME}` stays exactly that
  (assumption)); a write failure prints one line `could not save settings: <err>` and changes
  nothing else. On startup the existing merge order stands: user config, then startdir
  `yipyip.json` key-by-key on top, so a pinned project still overrides the user default.

## 3. Changed requirements

- R11 (`/exit`, `/quit`, double `Ctrl+C`): was "quit with exit code 0 and restore the terminal"
  → now also save every open tab's session first (R30); `yip --resume` is added to `--help` and
  `/help` mentions `/sessions`, the tab keys (R38) and shift+enter (R36).
- R11 (`/new`): was "discards conversation … and starts a fresh session" → now opens the fresh
  session in a new tab (R38) and leaves any saved file untouched (v3 keeps saving the new one on
  exit).
- R15 (UI): unchanged rendering; the tab bar (R38) is a new top line, the picker of R31/R35 and
  the question overlay (R39) are new modals, `/sessions` output is plain conversation text.
- R12 (provider request): unchanged request shape and host policy; failures before the first
  response byte now retry per R37 (a change of failure behaviour, not of the request).
- v1 `Alt+Enter` (newline): unchanged, now joined by `Shift+Enter` (R36).

## 4. Removed requirements

- none

## 5. Constraints & environment changes

- New package `internal/session`: `Store` (dir `<startdir>/.yipyip/sessions`), `Save`, `Load`,
  `List` (newest first), `Delete`, `SessionData` struct with the R29 fields; all writes atomic
  (write temp + `os.Rename`); all paths through `internal/tools`' confinement; `internal/agent`
  gains only `Session.Export() SessionData`-compatible accessors (history, counters, model,
  effort, mode) and `Session.Restore(data)`-shaped restore — or the export/import lives wholly
  in `internal/session` reading `Session` accessors, so `internal/agent` needs no new imports of
  `internal/session` (assumption: import direction `internal/session` → `internal/agent`, never
  the reverse).
- `internal/provider`: retry logic lives in `client.go` only; `transport.go` (host restriction,
  security-critical) is not modified and stays the only place that builds HTTP clients. No new
  hosts, no new secret env vars, no network beyond the existing provider hosts.
- `internal/tools`: the `question` tool joins the registry; its result comes from the TUI (user
  input), the same waiting pattern as the approval gate in `internal/agent/approval.go`. The
  `bash` tool and file tools are unchanged.
- `internal/config`: gains a settings-write path for exactly the keys `model`, `effort`, `mode`
  (atomic, key-preserving, secret-free). The load/merge path is unchanged.
- No new third-party packages. No new live systems. Tests stay offline (fake provider —
  extended to script connect/429/5xx failures before the first byte — temp dirs, temp `HOME`).
- Docs: `docs/usage.md` gains "Sessions", "Tabs", "Questions" and "Retry" sections; `README.md`
  stays untouched (protected) — its v1 "no session persistence" wording in §5/§9 is superseded
  by v3.
- Protected as in v2 (`prompts/`, `.claude/`, `README.md` §1–9, `Plan.md`, `versions/`,
  `scripts/verify/`); `CLAUDE.md` may gain lines about `internal/session` and the new keys
  (outside its Delegation section).

## 6. Acceptance criteria

- [ ] `make check` exits 0 [all]
- [ ] `go test ./internal/session`: Save→Load round-trip of every field; atomic-write leftover
      temp files absent; `version` 0 and 2 rejected; invalid JSON rejected; unknown fields
      ignored; List newest-first; Delete removes; confinement: a start dir with a symlinked
      `.yipyip` pointing outside is refused (negative); no API-key material in the file
      (`SECRET123` absent) [R29, R33, R16]
- [ ] `go test ./internal/tui -run ExitSave`: `/exit` with one non-empty tab writes one file;
      `/exit` with an empty conversation writes nothing; double `Ctrl+C` saves all tabs; a save
      failure shows `could not save session` and still quits [R30, R38]
- [ ] `go test ./internal/tui -run Sessions`: `/sessions` lists newest-first with
      title/updated/tokens/model, `no saved sessions` when empty, `… +N more` at >10,
      `/sessions <id>` loads history+counters+queue→draft into the current tab and drops live
      markers, `/sessions rm` asks `y/n`, `n` keeps the file, the current id is not offered for
      rm, `/sessions rm <id>` deletes [R32]
- [ ] `go test ./internal/tui -run Resume`: picker shows newest first with title/ago/tokens/
      model, `Enter` resumes (conversation + counters + history + queue→draft, in a new tab
      when there is room), `Esc` exits without starting, empty dir → new session, `Up`/`Down`
      move [R31, R35]
- [ ] `go test ./internal/tui -run Newline`: alt+enter and a `shift+enter` key message each
      insert `\n` in the input field [R36]
- [ ] `go test ./internal/provider -run Retry`: a server failing 9 times with connection
      resets/429/5xx then succeeding succeeds on the 10th attempt with the scripted waits;
      a 400 gets no retry; a failure after the first SSE byte gets no retry; every retried
      request goes to the original host (negative: a request to any other host is never made)
      [R37]
- [ ] `go test ./internal/tui -run Tabs`: `Ctrl+T` adds a tab (title segment appears, becomes
      current), 8-tab limit prints `tab limit reached`, `Ctrl+W` saves the tab's session and
      switches to the neighbour, last-tab close = `/exit` path, `Ctrl+Tab`/`Ctrl+Shift+Tab`
      cycle, `Alt+1`–`Alt+8` jump; `Tab` still completes and `Shift+Tab` still toggles the mode
      (negative: old keys unchanged); switching tabs swaps conversation/counters/history
      [R38]
- [ ] `go test ./internal/tui -run Question`: radio overlay for `multiple: false` — `↑`/`↓`
      move, `Space`/`Enter` return the chosen label; checkbox overlay for `multiple: true` —
      `Space` toggles, `Enter` returns the labels joined `, `; free text row returns the typed
      answer; `Esc` returns `cancelled`; the turn is not finished while the overlay waits
      [R39]
- [ ] `go test ./e2e -run Session`: full flow with the fake provider — turn 1 (tool call +
      result), `/exit`, restart, `--resume <id>`, turn 2 continues: request 2's messages =
      request 1's messages + turn-2 user message; same `reasoning_effort`, same model; counters
      continue (in/out/total); a `SECRET123` run stores no secret in the file; the file lives
      under `<startdir>/.yipyip/sessions/` [R34, R33, R16]
- [ ] `go test ./e2e -run Question`: scripted turn where the model calls `question`, the test
      answers via key messages, and the recorded follow-up request contains the answer as the
      tool result [R39]
- [ ] `go test ./e2e -run Retry`: fake provider rejects twice with 503 then streams; the turn
      completes and exactly 3 requests were recorded [R37]
- [ ] `go test ./internal/tui -run Settings` and `go test ./internal/config -run Settings`:
      changing model/effort/mode in the TUI writes the three keys to the user config
      atomically, other keys (incl. unknown ones and an `{env:NAME}` api key) survive
      byte-for-byte, secrets never appear in the file; a new session starts with the stored
      values; a write failure shows `could not save settings` [R40, R16]
- [ ] `go test ./internal/tui -run Help` and `go run ./cmd/yip --help` mention `--resume`,
      `/sessions`, the tab keys and shift+enter [R11 changed, R36, R38]
- [ ] `grep -n '\-\-resume' cmd/yip/main.go` matches and `grep -rn 'os.Rename' internal/session` matches [R29 atomic]
- [ ] `git diff --stat 9519824..HEAD -- internal/provider/transport.go internal/tools` prints
      nothing (9519824 = last v2 commit; transport and the file/bash tools are untouched)
      [R37 scope, security]
- [ ] `go test ./... -run Secret` exits 0 [R16]
- [ ] `grep -rn 'openrouter.ai' --include='*.go' .` matches only `_test.go` files [R18]

## 7. Out of scope

- Resume across machines or directories (sessions are per start dir); cloud sync; encryption of
  session files; multiple concurrent writers/locking; saving on every turn (only on exit);
  autosave on crash/SIGKILL; exporting to Markdown; a global `~/.config` session store; session
  branching/forking; per-message undo of a resumed session; mouse interaction with the tab bar;
  splitting the terminal into panes; retrying a stream after the first byte; changing the host
  policy or `transport.go`; persisting the input draft.
