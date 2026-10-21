# yipyip v4: skills, jev decisions, mouse-friendly input and tabs

## 1. Goal

v4 adds user skills as slash commands, lets the small decision model jev (TypeSafe System One) make
classification-style decisions to save main-model tokens, and polishes the TUI: click-to-cursor and
select-to-copy in the input, a smaller tab hotkey set with clickable `+` / `×` and a bounded tab bar,
and question prompts rendered inline in the conversation. The Shift+Enter newline of v3 is removed.

## 2. New requirements

- R41. Input click: a left click inside the input field moves the text cursor to the clicked cell
  (clamped to the end of that line / the text); a click outside the input does not move it.
- R42. Input select-to-copy: dragging the left button inside the input highlights the selection
  and on release copies the selected text exactly like R21 (OSC 52 + `wl-copy`/`xclip`/`xsel`,
  `N characters copied` notice, redacted text only); a click without drag copies nothing.
- R43. Tab buttons: each tab segment ends with a clickable `×` that closes that tab (R38 close
  path, session saved first); a `+` after the last segment opens a new tab like `Ctrl+T`. At the
  limit of 8 the `+` is not drawn and `Ctrl+T` still prints `tab limit reached`.
- R44. Tab bar bounds: the tab bar is always exactly 1 row. Titles are truncated (`…`) so that
  all segments fit the width (assumption: minimum 8 cells per title); if even that does not fit,
  segments left of the current tab collapse to `…`; the current tab is always visible.
- R45. Skills: skills are `<name>/SKILL.md` folders (frontmatter `name`, `description`, then the
  body) loaded from `~/.config/yipyip/skills/` and `<startdir>/.yipyip/skills/`; the same name in
  both: the project skill wins. A missing folder is fine. `name` defaults to the folder name and
  must match `^[a-z0-9-]+$`; others are skipped with one warning line. Frontmatter is parsed with
  the standard library only (`key: value` lines) (assumption). `/<name> [args]` sends the body as
  a user message with `args` appended after a blank line. A skill adds no permissions: tools and
  approval behave as usual. Skill files are only read, never written; the text goes through the
  redactor before view or session file. Built-in commands win over a skill of the same name.
- R46. `/skills`: lists loaded skills (name, description, `global` or `project`, and
  `shadowed by built-in` where it applies); skills are re-read on every `/skills` and at startup,
  and appear in the `/` suggestions with their description. Without skills: `no skills found`.
- R47. Jev client and config: config key `jev` = `{"baseURL","apiKey","model","min_confidence"}`
  (defaults: `https://api.typesafe.ai`, none, `jev-latest`, `0.7`; `apiKey` normally
  `{env:TYPESAFE_API_KEY}`). `POST <baseURL>/v1/systemone` with `Authorization: Bearer <key>` and
  body `{model, state, questions}` (types `choice`, `score`, `noul`); answers are parsed per the
  documented response (`choice`+`confidence`, `score`, `noul`). A 422 or any failure is an
  error value, never a crash. The jev host is allowed by the R3 transport only while `jev` is
  configured. `state` is redacted before sending and contains only what the question needs.
- R48. `/jev`: toggles jev on/off (`jev on` / `jev off` line); without a `jev` config or key it
  prints `jev is not configured` and stays off. While on the status line shows `jev`; `/jev stats`
  shows the in-memory counters: calls answered by jev, fallbacks, jev input tokens, and main-model
  tokens per turn split into `question`, `routing` and `other` (no disk logging) (assumption).
- R49. Jev decides questions: with jev on, a `question` tool call with `multiple=false` and at
  least 2 options is answered by jev (`choice`, state = question + conversation tail of at most
  2000 chars (assumption)) when `confidence` ≥ `min_confidence`; the tool result is the chosen
  label. Free-text-only, `multiple=true` or low-confidence questions go to the user as before.
- R50. Jev routing: with jev on and `jev.route` = `{"easy": "<provider/model>", "hard": "<provider/model>"}`
  configured, at the start of each user turn jev rates difficulty (`score`, 3 levels); score < 1
  uses `easy`, else `hard`; the choice holds for the whole turn and the status line shows it.
  Unknown models in `route` are rejected at config load with a message naming the key.
- R51. Jev risk screen (auto mode only): with jev on, before a `bash`/`write_file`/`edit_file`
  call in auto mode jev answers a `noul` risk question; probability ≥ 0.5 turns the call into
  the manual approval prompt for that call. Jev can only add a prompt, never remove one: manual
  mode and `approval.go` decisions are unchanged and no jev answer approves a call.
- R52. Jev fallback: on error, timeout (10 s, assumption) or low confidence every jev use
  falls back to the old behaviour (question to the user, configured model, auto mode as is);
  a turn never waits on jev longer than the timeout.

## 3. Changed requirements

- R36: was `shift+enter` inserts a newline where the terminal reports it → now `shift+enter`
  has no special meaning; only `alt+enter` inserts a newline. The modifyOtherKeys request and the
  CSI mapping for it are removed; `/help` lists only `Alt+Enter`.
- R38: was `Ctrl+Tab`/`Ctrl+Shift+Tab` cycle and `Alt+1`–`Alt+8` jump → now only `Ctrl+T` (new)
  and `Ctrl+W` (close) remain as tab hotkeys; `Tab` and `Shift+Tab` (mode toggle) are unchanged.
- R39: was the question renders as a centered overlay → now it renders as a block inside the
  conversation transcript (below the last message, scrolling with it, replaced by the answer
  line `Q: … → A: …` once answered); keys, results and cancel behaviour are unchanged.
- R3: was only hosts of configured providers are reachable → now also the host of a configured
  `jev.baseURL`.

## 4. Removed requirements

- R36 (Shift+Enter newline handling), the `Ctrl+Tab`, `Ctrl+Shift+Tab` and `Alt+<N>` parts of R38.

## 5. Constraints & environment changes

- No new packages; `go.mod` unchanged (frontmatter parsed by hand).
- New secret env var name: `TYPESAFE_API_KEY` (read only through `{env:NAME}`; tests never need it).
- New live system: `api.typesafe.ai`, contacted only by the jev client and never in tests (they use
  a fake System One endpoint in `internal/provider/providertest`).
- Security-critical code keeps its places: host check in `transport.go`, redaction in `redact.go`,
  approval in `approval.go` (R51 only adds a call into it).

## 6. Acceptance criteria

- [ ] [R41] Test: click at cell (x, y) in a 3-line input puts the cursor there; click past line end clamps; click outside leaves it.
- [ ] [R42] Test: drag in the input copies the selected text; click copies nothing; `SECRET123` never copied.
- [ ] [R43] Test: click on `×` closes that tab and saves it; click on `+` opens a tab; at 8 tabs no `+`.
- [ ] [R44] Test: with 8 long titles the bar is 1 row at widths 60 and 120 and the current tab is visible.
- [ ] [R45] Tests: global only, project only, project overrides global, bad name/frontmatter skipped, args appended, built-in wins, `SECRET123` negative.
- [ ] [R46] Test: `/skills` output (both sources, shadow note, empty case); `/` suggestions contain a skill.
- [ ] [R47] Tests against the fake endpoint: request shape and auth header, three answer types, 422, foreign host refused, jev host refused when `jev` is unset.
- [ ] [R48] Tests: `/jev` toggles, unconfigured message, `/jev stats` counters.
- [ ] [R49] Test: confident choice answers the question without a user prompt; low confidence, multi-select and 1-option cases still prompt.
- [ ] [R50] Test: score < 1 selects `easy`, else `hard`, for the whole turn; bad `route` model rejected.
- [ ] [R51] Test: risk ≥ 0.5 shows the prompt in auto mode; manual mode and jev "safe" answers change nothing.
- [ ] [R52] Tests: error, timeout and low confidence each fall back; the turn finishes.
- [ ] [R36] Test: `shift+enter` inserts nothing, `alt+enter` inserts a newline; `/help` has no Shift+Enter.
- [ ] [R38] Test: `Ctrl+Tab`, `Ctrl+Shift+Tab`, `Alt+1` do nothing; `Ctrl+T` / `Ctrl+W` work.
- [ ] [R39] Test: `View()` shows the question inline in the transcript, answer line after Enter, no overlay.
- [ ] `make check` from `Plan.md` exits 0 (v1–v3 keep working).

## 7. Out of scope

Image/`box` questions, jev for chat or code generation, disk logging of tokens, skill install
commands or remote skills, skills that grant tool permissions, per-tab model selection, any real
network call in tests.
