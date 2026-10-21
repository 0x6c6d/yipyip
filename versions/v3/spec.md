# yipyip v3: sessions that survive a restart

## 1. Goal

v3 makes a yipyip session durable and resumable: on `/exit` (and on a clean quit) the session is
saved to a file under the start directory, and `yip --resume` (alias `-c`, Claude-Code style)
reopens it with the full conversation, token counters, mode, effort, model, input history, paste
store and message queue intact, so work continues where it stopped. Sessions are plain JSON,
listed and picked interactively, and removable. No change to the provider client, network policy
or file tools.

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
- R30. Save on exit: `/exit`, `/quit` and double `Ctrl+C` write the file (unless the conversation
  is empty: nothing is written, no error), restore the terminal, exit 0. The write goes through
  `internal/tools`' confined writer (start-dir confinement, `PathGuard`); the file is written
  atomically (temp file + rename in the same directory). Secrets are never stored: keys live
  only in env vars, the session file holds no API key (R16); `{env:NAME}` references in config
  are re-resolved at resume, not persisted. A failed save prints one line `could not save
  session: <err>` to the conversation and still exits 0 (assumption: exit 0, like v1).
- R30a. Save on exit-path errors: if the program exits via a startup error there is no session
  to save (unchanged v1 behaviour); this requirement adds no new behaviour, it records that
  v1 exit codes are unchanged (assumption).- R31. `--resume [id]` (alias `-c`): with an id, reopen that session; without, pick
  interactively when stdin is a TTY: `yip --resume` shows the picker of saved sessions (newest
  first, `title`, relative `updated` time (assumption: `2h ago` style via
  `time.Duration.String`-like human short form), tokens, model; `Up`/`Down` move, `Enter` opens,
  `Esc` exits without starting). A non-TTY or empty sessions dir starts a new session
  (assumption). `yip --resume <bad-id>` exits 1 with `unknown session <id>` on stderr (naming no
  secret). The resumed session keeps id, created, conversation, counters, mode/effort/model,
  history/pastes/queue (queue becomes the draft input, joined `\n`, since no turn is running at
  resume (assumption)). `/new` on a resumed session starts fresh (new id) and keeps the old file
  (assumption: nothing is deleted silently); a file removed on disk disappears from the picker.
- R32. `/sessions`: lists saved sessions of the start dir (`id`, title, `updated` (local time
  `15:04  Jan 2`), tokens total, model) in the conversation, newest first, at most 10 with
  `… +N more`; `/sessions <id>` loads that session into the running program (replacing
  conversation, counters, mode/effort/model, history/pastes, queue → draft, marker lines dropped
  (assumption: live markers are turn state, not conversation content)); `/sessions rm <id>`
  deletes the file (confirmation prompt `y/n`, `n` keeps it; the current session's own file is
  never listed for rm (assumption)); `/sessions` with no sessions prints `no saved sessions`.
- R33. Privacy & security: session files live only under `<startdir>/.yipyip/sessions/`, are
  written only by the save path (never a log file), contain no API keys and no redacted-view
  placeholders; tool results are stored as sent to the model (which is the un-redacted tool
  output (assumption: the model must see the same content on resume), so a session file can
  contain file contents and command output, exactly like the provider already receives them).
  `bash` receives an env without the config's `{env:}` variables (unchanged) and the save path
  never runs a command. `.yipyip/` is added to the repo's `.gitignore` (assumption). A
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

## 3. Changed requirements

- R11 (`/exit`, `/quit`, double `Ctrl+C`): was "quit with exit code 0 and restore the terminal"
  → now also save the session first (R30); `yip --resume` is added to `--help` and `/help`
  mentions `/sessions`.
- R11 (`/new`): was "discards conversation … and starts a fresh session" → now also gives the
  new session a fresh id and leaves any saved file untouched (v3 keeps saving the new one on
  exit).
- R15 (UI): unchanged rendering; the picker of R31/R35 is a new modal, and `/sessions` output is
  plain conversation text.

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
- No new third-party packages. No new live systems, no new secret env vars, no network beyond
  the existing provider hosts. Tests stay offline (fake provider, temp dirs, temp `HOME`).
- Docs: `docs/usage.md` gains a "Sessions" section; `README.md` stays untouched (protected) —
  its v1 "no session persistence" wording in §5/§9 is superseded by v3.
- Protected as in v2 (`prompts/`, `.claude/`, `README.md` §1–9, `Plan.md`, `versions/`,
  `scripts/verify/`); `CLAUDE.md` may gain a line about `internal/session` (outside its
  Delegation section).

## 6. Acceptance criteria

- [ ] `make check` exits 0 [all]
- [ ] `go test ./internal/session`: Save→Load round-trip of every field; atomic-write leftover
      temp files absent; `version` 0 and 2 rejected; invalid JSON rejected; unknown fields
      ignored; List newest-first; Delete removes; confinement: a start dir with a symlinked
      `.yipyip` pointing outside is refused (negative); no API-key material in the file
      (`SECRET123` absent) [R29, R33, R16]
- [ ] `go test ./internal/tui -run ExitSave`: `/exit` on a non-empty conversation writes a file
      with the conversation; `/exit` on an empty conversation writes nothing; double `Ctrl+C`
      saves; a save failure shows `could not save session` and still quits [R30]
- [ ] `go test ./internal/tui -run Sessions`: `/sessions` lists newest-first with
      title/updated/tokens/model, `no saved sessions` when empty, `… +N more` at >10,
      `/sessions <id>` loads history+counters+mode+effort+model and drops live markers,
      `/sessions rm` asks `y/n`, `n` keeps the file, the current id is not offered for rm,
      `/sessions rm <id>` deletes [R32]
- [ ] `go test ./internal/tui -run Resume`: picker shows newest first with title/ago/tokens/
      model, `Enter` resumes (conversation + counters + history + queue→draft), `Esc` exits
      without starting, empty dir → new session, `Up`/`Down` move [R31, R35]
- [ ] `go test ./e2e -run Session`: full flow with the fake provider — turn 1 (tool call +
      result), `/exit`, restart, `--resume <id>`, turn 2 continues: request 2's messages =
      request 1's messages + turn-2 user message; same `reasoning_effort`, same model; counters
      continue (in/out/total); a `SECRET123` run stores no secret in the file; the file lives
      under `<startdir>/.yipyip/sessions/` [R34, R33, R16]
- [ ] `go test ./internal/tui -run Help` and `go run ./cmd/yip --help` mention `--resume`,
      `/sessions` [R11 changed]
- [ ] `grep -n '\-\-resume' cmd/yip/main.go` matches and `grep -rn 'os.Rename' internal/session` matches [R29 atomic]
- [ ] `go test ./... -run Secret` exits 0 [R16]
- [ ] `git diff --stat 9519824..HEAD -- internal/provider internal/tools` prints nothing (9519824 = last v2 commit) [scope]
- [ ] `grep -rn 'openrouter.ai' --include='*.go' .` matches only `_test.go` files [R18]

## 7. Out of scope

- Resume across machines or directories (sessions are per start dir); cloud sync; encryption of
  session files; multiple concurrent writers/locking; saving on every turn (only on exit); autosave
  on crash/SIGKILL; exporting to Markdown; a global `~/.config` session store; session
  branching/forking; per-message undo of a resumed session; changing `internal/provider` or
  `internal/tools`.
