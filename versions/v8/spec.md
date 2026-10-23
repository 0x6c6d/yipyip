# yipyip v8: jev_rate, jev rates a task on request

## 1. Goal

A new tool `jev_rate` lets the model ask jev how hard a piece of coding work is and get back
`easy`, `medium` or `hard`. The supervisor loop of `r_yip.md` uses it: the task generator (a subagent)
asks jev once per task and writes the tier into `Tasks.md`. Jev so far only routes user turns (v5);
this adds a second, explicit use of the same score question. It sends text to the jev host, so it is
labeled data/privacy-related.

## 2. New requirements

- R68. Tool `jev_rate`, parameter `text` (string, required, not empty after trimming, at most 8,000 characters). It is registered for the main agent and for subagents exactly when a `jev` block with an `apiKey` is configured (`Session.JevConfigured()`), whether `/jev` is on or off; without such a block it is absent from the tool list sent to the model. An invalid `text` returns an error result and sends nothing.
- R69. Request: one request to jev per call, bounded by the jev timeout (10 s), with the same score question as turn routing (levels easy/medium/hard, question name `taskDifficulty`, instructions "Rate how hard the described coding task is to carry out well"). The state is `task: ` + the text, redacted by the config redactor and cut to `JevStateLimit` (2,000 runes); the API key is never sent in the state or shown. It uses the existing jev client and host only; no new host, no new HTTP client.
- R70. Result (a normal, non-error tool result, so the caller always gets a tier): line 1 is exactly `tier: easy`, `tier: medium` or `tier: hard`, from the score rounded and clamped as in turn routing (`scoreTier`); line 2 is `confidence: <0.00-1.00>`. When the text was cut a line `truncated: first 2000 characters sent` follows. **Unsure means medium:** a jev error, a timeout, an invalid or malformed answer, or a confidence below `jev.min_confidence` (default 0.7) returns `tier: medium` and a line `fallback: <jev error|timeout|invalid answer|low confidence>`, never `easy` or `hard`. A cancelled turn returns an error result `cancelled`. The result never contains the API key.
- R71. The tool is not approval-gated (not in `gatedTools`, any mode, also for subagents). Turn routing is unchanged: `routeTurn` still needs `/jev` on and a `jev.route`, and `/jev` on/off does not affect `jev_rate`. Jev still never answers `question` calls and never screens risk (R49, R51 stay in force).
- R72. Counters, per tab, in memory: `/jev stats tasks` prints exactly these lines (`N` integers): `tasks rated easy: N`, `tasks rated medium: N`, `tasks rated hard: N`, `task rating fallbacks: N`, `task rating input tokens: N`; a fallback counts as rated `medium` and as a fallback. Without a configured jev: `jev is not configured`. The output of `/jev stats` and `/jev` and their error texts stay byte for byte as in v5.
- R73. Docs: `docs/usage.md` documents `jev_rate` (parameter, result format, `medium` when unsure, independent of the `/jev` toggle, 2,000-character limit, `/jev stats tasks`); the `internal/agent` and `internal/tui` lines of `CLAUDE.md` mention it; neither text still says that jev "only routes turns".

## 3. Changed requirements

- v5 (jev narrowed to turn routing, R53-R56): was "jev only routes each user turn" → now jev also rates a task on request through `jev_rate` (R68-R72); turn routing, its stats and its fallbacks (session model) are unchanged.

## 4. Removed requirements

none

## 5. Constraints & environment changes

- No new packages, secrets, hosts or tools to install. Tests use the fake System One endpoint of `providertest`, never the real host.
- Use only the `score` question type. The v5 negative checks must keep passing: no `JevChoice`, `SetRiskScreen`, `jevRisk`, `jevAnswer`, `jevQuestionState`, `turnHistory` or `QuestionTokens` anywhere in `internal/`.
- `askJev` and `ErrJevOff` keep their meaning for turn routing; the tool uses its own path that does not need the toggle (assumption: a small shared helper for the redaction, cut and timeout).
- Security-critical files (`transport.go`, `pathguard.go`, `bash.go`, `approval.go`, `redact.go`) are not changed; `approval.go` stays byte for byte as at the v7 start (its guard in the v5 `T799.sh`).
- `README.md` sections 1-9 are not changed in v8 (assumption: the jev paragraph of the Usage part stays; docs go to `docs/usage.md`).

## 6. Acceptance criteria

- [ ] `go test ./internal/agent -run JevRate -count=1 -v` exits 0 with `TestJevRateRegistered` (main agent and subagent offer it with jev configured, also with `/jev` off; absent without a `jev` block), `TestJevRateParams` (empty, blank and 8,001-character text return an error result and the fake receives no request) [R68].
- [ ] The same run shows `TestJevRateRequest` (fake receives one `taskDifficulty` score question, state starts `task: `, a `SECRET123` in the text arrives redacted, 3,000 runes arrive cut to 2,000, the key appears nowhere) [R69].
- [ ] The same run shows `TestJevRateTiers` (scores 0, 0.4, 0.6, 1, 1.6, 2, 7, -3 give easy, easy, medium, medium, hard, hard, hard, easy; line 1 and line 2 exact; `truncated:` line only when cut) and `TestJevRateFallback` (HTTP 500, 10 s timeout via `SetJevTimeout`, malformed answer and confidence 0.3 each give `tier: medium` plus the right `fallback:` reason, none gives easy or hard, key absent) [R70].
- [ ] The same run shows `TestJevRateNotGated` (manual mode runs it without an approval prompt, main agent and subagent) and `TestJevRateRoutingUnchanged` (with `/jev` off no turn is routed; with `/jev` on and a route the turn routing counters behave as in v5) [R71].
- [ ] `go test ./internal/tui -run 'JevStats' -count=1 -v` exits 0 with the v5 tests unchanged and a new `TestJevStatsTasks` that prints the five lines exactly, counts a fallback as `medium`, and prints `jev is not configured` without jev [R72].
- [ ] `go test ./e2e -run JevRate -count=1` exits 0: a scripted main model calls `jev_rate`, a scripted subagent calls it, the fake jev sees two requests, and `/jev stats` output afterwards is unchanged [R68, R71, R72].
- [ ] `grep -n 'jev_rate' docs/usage.md CLAUDE.md` finds the doc and both `CLAUDE.md` lines, `grep -n 'stats tasks' docs/usage.md` finds the new command, and `grep -n -e 'only routes' -e 'routing only' docs/usage.md CLAUDE.md` prints nothing [R73].
- [ ] `grep -rn -e JevChoice -e SetRiskScreen -e jevRisk -e jevAnswer -e QuestionTokens internal` prints nothing and `git diff --quiet <v8 start commit> -- internal/agent/approval.go internal/provider/transport.go` exits 0 [R71].
- [ ] `go test ./internal/provider -run Jev -count=1` exits 0 (jev client unchanged) and `make check` (from `Plan.md`) exits 0: earlier versions keep working.

## 7. Out of scope

- Using `jev_rate` automatically (at dispatch, in `spawn_agent`, or in the loop prompts): the loop prompts of `r_yip.md` call the tool, yip does not. Other question types, rating more than one text per call, rating by file path.
- Changing `/jev`, `/jev stats`, turn routing, the `jev.route` rules or the session-model fallback of turn routing.
- Persisting counters, a config key to turn the tool off, per-session limits on the number of ratings.
