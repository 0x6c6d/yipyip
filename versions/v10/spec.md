# v10: Crash resistance

## 1. Goal

A long agent loop must survive faults and leave evidence when it does not. Today a panic outside a
tool call prints only to the terminal, a recovered panic drops the whole in-flight turn, nothing is
logged, and sessions are written only on a clean exit, so a crash loses all work since the last
exit. v10 adds a crash log, keeps the work of a turn across a recovered panic, and autosaves
sessions while running.

## 2. New requirements

- R78. Crash log: when a panic ends the program (anywhere, including the TUI goroutine and agent
  goroutines), `yip` appends a record to `<startdir>/.yipyip/crash.log` (dir mode 0700, file mode
  0600) before exiting with code 2. A record has: RFC 3339 time, `yip --version` string, the panic
  value, the full stack trace of the panicking goroutine, and the id of the active tab's session if
  it was already saved. The terminal is restored first, and stderr gets one line
  `yip: crashed, details in <path>`. If the log cannot be written, the record goes to stderr.
- R79. Recovered panics are logged: every recovered panic (tool call, main-agent turn, subagent)
  appends a record of the same format (marked `recovered`) to the same file, with its stack trace
  taken at the recover site; the conversation message stays as today. The log is capped: when it
  exceeds 1 MiB, the oldest records are dropped so the file keeps at most 512 KiB.
- R80. The log never holds secrets or conversation content: the panic value and the stack are
  redacted with the configured `Redactor`, and no prompt, history, tool argument or tool output is
  written, only the panic value and the stack.
- R81. Turn checkpoints: the main agent commits its working history to the session after every
  completed model round (the assistant message plus all its tool results, never an assistant tool
  call without its results). After a recovered panic in a turn, the history keeps everything up to
  the last checkpoint, plus the user message; the turn ends with the existing error line.
- R82. Autosave: while a tab has a non-empty conversation, `yip` saves its session file (same
  format, same id) (a) when a turn ends and (b) after a checkpoint if the last autosave is at least
  1 min old (assumption). The write happens off the UI goroutine through the existing `Store`
  (atomic, confined to `<startdir>/.yipyip/sessions`). A failure appends no block per attempt; the
  first failure per tab shows one `could not autosave session: ...` line, and later failures stay
  silent until a save works again.
- R83. A session autosaved during a running turn is stored as `interrupted` (as in R30 for a turn
  cut by the exit) and its conversation ends at the last checkpoint, so `--resume` of it sends a
  valid request (no dangling tool call).
- R84. A subagent panic does not abort its parent turn: the parent gets the existing failed-subagent
  result, and the panic is logged (R79). A panic in a render path (`View`) of one block is shown as
  `[render error]` for that block instead of crashing the program (assumption: Bubble Tea's
  `Update`/`View` stay unguarded otherwise; R78 covers them).
- R85. Session retention: the `Store` offers `Prune(keep int, protect ...string)`, which deletes the
  oldest `<id>.json` files of `<startdir>/.yipyip/sessions` so that at most the 25 newest (by the
  `updated` order of `List`) remain; the ids of all open tabs and of the file just saved are never
  deleted, even if that leaves more than 25. Files whose name is not `<12 hex>.json` are never
  touched. `yip` calls it once at startup and after each successful save (autosave and exit), off
  the UI goroutine; a prune error is silent. Nothing else deletes sessions except `/sessions`.

## 3. Changed requirements

- R30: was "`/exit`, `/quit` and double `Ctrl+C` write the file of every open tab" → now these still
  do, and the file may already exist from an autosave (same id, overwritten).
- README section 5 data/privacy: was "no logs containing prompts or keys are written to disk" → now
  `crash.log` is written, containing only redacted panic values and stack traces (R80).

## 4. Removed requirements

none

## 5. Constraints & environment changes

- No new packages. The crash-log writer is stdlib only, in a new `internal/crashlog` package;
  `cmd/yip`, `internal/agent`, `internal/tools` and `internal/tui` call it and never re-implement
  it. `crashlog` is added to the security-critical list in `CLAUDE.md` (architecture section only;
  workers never touch its "Delegation workflow" section).
- `.yipyip/` is already in `.gitignore`; the log lives under it.
- No live systems; tests use the `httptest` fake provider and inject panics through test seams.

## 6. Acceptance criteria

- [ ] [R78] A test runs the `yip` binary (test seam: a hidden env var forces a panic in the UI
      goroutine) and finds exit code 2, a `crash.log` with time, version, panic value and a stack
      containing the panicking function name, and the stderr line `yip: crashed, details in`.
- [ ] [R79] A test with a panicking tool shows a `recovered` record in `crash.log` and the existing
      `tool X failed: panic` text; a log over 1 MiB is cut to at most 512 KiB, newest records kept.
- [ ] [R80] A test panics with a configured secret (`SECRET123`) in the panic value and in the
      conversation; `crash.log` contains neither `SECRET123` nor any prompt text.
- [ ] [R81] An e2e test makes the 3rd model round panic after two tool rounds; afterwards
      `Session.History()` contains both completed rounds and no assistant tool call without results.
- [ ] [R82] An e2e test: after a turn ends the session file exists without exit; mid-turn autosaves
      respect the 1 min spacing (fake clock) and never run on the UI goroutine; a read-only sessions
      dir yields exactly one `could not autosave session` line across 3 failing saves.
- [ ] [R83] A session file autosaved mid-turn has `turn.interrupted: true`, loads with `--resume`
      and the next request to the fake provider is accepted (no dangling tool call).
- [ ] [R84] A subagent that panics returns a failed result and the parent turn finishes; a block
      whose render panics shows `[render error]` while the other blocks stay visible.
- [ ] [R85] A test with 30 session files (distinct `updated`) and 2 protected old ids: `Prune(25,
      ...)` leaves the 25 newest plus the 2 protected, deletes the rest, and leaves a stray
      `notes.txt` and `abc.json` in the directory; an e2e test shows startup pruning 30 → 25 and a
      session open in a tab is never pruned.
- [ ] [R30] `/exit` after an autosave leaves exactly one file for that conversation (same id).
- [ ] `make check` still exits 0 (earlier versions keep working) and `bash` approval, path guard and
      transport tests are unchanged.

## 7. Out of scope

- Crash reporting or upload of any kind (no network), a debug/verbose log of normal operation,
  rotation beyond the size cap, age-based or size-based session retention (R85 counts only),
  a config key for the retention count (25 is fixed), automatic restart or supervision of `yip`, resuming a turn in
  progress (R83 only makes the session resumable at its last checkpoint), handling of `SIGKILL`/OOM
  (no code can log those; the checkpoints and autosave are what survives them).
