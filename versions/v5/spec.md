# yipyip v5: jev routing only, click-to-cursor fix, longer subagent runs

## 1. Goal

v5 narrows jev to one job: routing each user turn to the cheapest model that fits (three tiers instead
of two) to cut token cost. Jev no longer answers questions for the user or screens risk in auto mode.
It also fixes the input cursor not following a click and raises the subagent iteration limit to 500.

## 2. New requirements

- R53. Three-tier routing: `jev.route` = `{"easy","medium","hard"}`, each a `<provider>/<model>` id of a
  configured provider. The difficulty `score` has 3 levels: 0 → `easy`, 1 → `medium`, 2 → `hard`
  (scores are rounded and clamped to 0..2). `medium` is optional: when absent, score 1 uses `hard`
  (v4 behavior); `easy` absent → `hard`. Only `hard` is required for routing to be active
  (assumption). Unknown or unconfigured models are rejected at config load naming the key
  (e.g. `jev.route.medium`). The choice holds for the whole turn and the status line shows it.
- R54. Subagent iteration limit: a subagent loop stops after 500 model calls (main agent keeps 50);
  the limit error text names the number actually used.
- R55. Route examples: `README.md` (config section) shows a three-tier `jev.route` example with what
  each tier is for (easy: questions, lookups, small local edits; medium: changes across a few files;
  hard: design, refactors, deep bugs), and `/jev` without args prints the active route, e.g.
  `jev on · easy=a/x medium=b/y hard=c/z`, or `jev on · no route configured`.

- R56. Stats: `/jev stats` prints one line per counter, in this order (`N` = integer):
  `turns routed easy: N`, `turns routed medium: N`, `turns routed hard: N`,
  `turns not routed: N` (jev off, no route, or fallback),
  `jev fallbacks: N` (routing calls that failed, timed out or were below `min_confidence`),
  `jev input tokens: N` (sent to jev),
  `main tokens easy: N`, `main tokens medium: N`, `main tokens hard: N`, `main tokens not routed: N`
  (main-model total tokens of the turns of that tier, subagents included (assumption)),
  and `avg main tokens per turn: easy N, medium N, hard N, not routed N` (integer, `-` for 0 turns)
  so the cost of each tier can be compared. Without a configured jev: `jev is not configured`.
  `QuestionTokens`, `Answered` and the `question`/`routing`/`other` categories are removed from
  `JevStats`.

## 3. Changed requirements

- R41: was a click in the input moves the cursor (not working in practice: the cursor stays where it
  was) → now the cursor really lands on the clicked cell, with the textarea's own cursor, including
  soft-wrapped rows, a scrolled input (more than 3 rows) and wide runes; the regression is covered
  by a test that sends a real `tea.MouseMsg` through `Update` and checks `Line()`/column.
- R6: was 50 iterations for every agent → now 50 for the main agent, 500 for subagents (R54).
- R50: was two tiers `easy`/`hard`, score < 1 → easy → now three tiers (R53).
- R52: was fallback for questions, routing and risk → now only routing falls back (error, timeout 10 s,
  low confidence → the session's model); a turn never waits on jev longer than the timeout.
- R48: was `/jev stats` shows answered calls, fallbacks, jev tokens and main-model tokens by category
  (question/routing/other) → now `/jev stats` reports routing only, current tab, in memory (see R56).

## 4. Removed requirements

- R49: delete jev answering of questions: every `question` tool call goes to the user, with jev on
  or off (`jevAnswer`, `jevQuestionState`, the turn-history context value, `JevChoice` use).
- R51: delete the jev risk screen (`jevRisk`, `Gate.SetRiskScreen` and its wiring, the `risk`
  question). `approval.go` decisions are exactly as before v4.

## 5. Constraints & environment changes

none. (No new packages, env vars or live systems; `api.typesafe.ai` is still contacted only by the
jev client and never in tests.)

## 6. Acceptance criteria

- [ ] [R53] Tests: scores 0/1/2 pick easy/medium/hard for the whole turn; missing `medium` → `hard`; bad model rejected naming `jev.route.medium`; a v4 two-key route still loads.
- [ ] [R54] Test: a scripted subagent that always calls tools stops at exactly 500 model calls; the main agent still stops at 50 [R6].
- [ ] [R55] Test: `/jev` output with and without a route; `grep -c "medium" README.md` ≥ 1 inside the jev config example.
- [ ] [R41] Test: real mouse click at cell (x, y) in a multi-line, a soft-wrapped and a scrolled input puts the cursor there; outside the input it does not move.
- [ ] [R49] Test: jev on with a confident `choice` fake answer still shows the question to the user; the fake endpoint receives no `answer` question; `grep -rn JevChoice internal/agent` finds nothing.
- [ ] [R51] Test: jev on, auto mode, fake `noul` 0.99 → the `bash`/`write_file`/`edit_file` call runs without a prompt and the fake endpoint receives no `risk` question.
- [ ] [R52] Tests: error, timeout and low confidence fall back to the session model and count one fallback.
- [ ] [R56, R48] Test: after scripted turns routed easy, medium, hard and one fallback, `/jev stats` prints exactly the lines above with the expected counts, tokens and averages; no `question` line; `grep -rn QuestionTokens internal` finds nothing.
- [ ] `make check` from `Plan.md` exits 0 (earlier versions keep working; v4 tests of R49/R51 are updated or removed with this spec).

## 7. Out of scope

Per-tier descriptions in the config, jev for chat or code generation, changing the main agent's 50
iterations, new jev question types, any real network call in tests.
