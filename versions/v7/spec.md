# yipyip v7: no model-call limit for the main agent

## 1. Goal

The main agent may run as many model calls in one turn as the task needs, so a supervisor session
(`/loop-coding`) can work through a whole `Tasks.md` in one turn. The code change already exists
(commit `4614ced`); this version makes it the specified behavior, brings the spec and docs in line
and removes the two verification scripts that still assert the old limit of 50. This relaxes a v1
rule on purpose (R6) and removes a cost guard, so it is labeled money-related.

## 2. New requirements

- R64. (money) The main agent has no per-turn limit on model calls: `agent.MaxIterations` is `0`, which means unlimited, and `runLoop` honors it. A main turn ends only on a reply without tool calls, a provider error after the retries of R37, or a cancel (`Esc`). A scripted provider that returns tool calls for 120 requests and then a final text produces exactly 121 requests, no `EvError`, and a history that ends with that final text. The error text `iteration limit (50) reached` can no longer occur for the main agent.
- R65. Cancel still works in a long turn: with the limit gone, cancelling the context of a turn that is in its 60th scripted tool round ends the turn within 2 s and leaves every tool call answered in the history.
- R66. Docs: `docs/usage.md` says that the main agent has no model-call limit per turn (`Esc` cancels) and that a subagent run stops after 500 model calls; neither `docs/usage.md` nor the `internal/agent` line of `CLAUDE.md` still names 50 for the main agent.
- R67. Verification consistency: `scripts/verify/v5/T401.sh` and `scripts/verify/v5/T799.sh` no longer require the main agent to stop at 50 (the `TestZZPosMain50` and `TestZZEvil` probes, the test name `TestMainIterationLimit`, the check `MaxIterations != 50`); they check R64 instead (main unlimited, subagent still exactly 500 with `iteration limit (500) reached`) and exit 0.

## 3. Changed requirements

- R6: was "repeats until a reply without tool calls or 50 iterations (assumption)" → now repeats until a reply without tool calls (R64); `Esc` cancels as before.
- R54: was "a subagent loop stops after 500 model calls (main agent keeps 50)" → now a subagent loop stops after 500 model calls and the main agent has no limit (R64).

## 4. Removed requirements

- R6 (part): the 50-iteration limit of the main agent and its error `iteration limit (50) reached`.

## 5. Constraints & environment changes

- none for tools, packages and secrets; no new live systems.
- Already done and not to be redone: `internal/agent/loop.go`, `internal/agent/loop_test.go` (`TestMainNoIterationLimit`), `docs/usage.md`, `CLAUDE.md`, `scripts/verify/v1/T403.sh` and its probe. Tasks only verify them and fix what the criteria below find.
- `README.md` sections 1-9 are updated by the supervisor to R6 above (R6 text and the acceptance line "iteration limit stops at 50").
- Exception to "finished versions stay untouched": only `scripts/verify/v5/T401.sh` and `scripts/verify/v5/T799.sh`, only to drop the main-50 assertions (R67). `versions/v1/` and `versions/v5/` spec and plan files stay as they are; they are history.
- `loop.go` is shared by main agent and subagents: the subagent limit of 500 must not change.

## 6. Acceptance criteria

- [ ] `go test ./internal/agent -run 'MainNoIterationLimit' -count=1` exits 0 and `grep -n 'const MaxIterations = 0' internal/agent/loop.go` finds a line [R64].
- [ ] `go test ./internal/agent -run 'MainNoIterationLimit|MainCancel' -count=1` exits 0, where `TestMainCancelLongTurn` cancels in the 60th tool round and checks the 2 s bound and that every tool call has a result [R65].
- [ ] `go test ./internal/agent -run SubagentIterationLimit -count=1` exits 0: still exactly 500 requests and `iteration limit (500) reached` [R54].
- [ ] `grep -nE 'main agent stops after 50|50 model calls per main turn|iteration limit \(50\)' docs/usage.md CLAUDE.md` prints nothing and `grep -n 'no limit' docs/usage.md` finds the new sentence [R66].
- [ ] `bash scripts/verify/v5/T401.sh` and `bash scripts/verify/v5/T799.sh` exit 0, and `grep -n 'TestMainIterationLimit\|TestZZPosMain50\|MaxIterations != 50' scripts/verify/v5/*.sh` prints nothing [R67].
- [ ] `bash scripts/verify/v1/T403.sh` shows no failure other than the "HTTP error shown" probe, which already failed before v7 (assumption: pre-existing, not part of this version) [R6].
- [ ] `grep -n 'iteration limit stops at 50\|or 50 iterations' README.md` prints nothing [R6].
- [ ] `make check` (from `Plan.md`) exits 0: earlier versions keep working.

## 7. Out of scope

- A configurable or optional main-agent limit (`max_iterations` in the config), a token or cost budget, a loop-detection guard. Cancelling with `Esc` is the only brake (assumption, as ordered by the user: "no limits").
- Changing the subagent limit of 500 or `max_subagents`.
- Fixing the pre-existing "HTTP error shown" probe failure of `scripts/verify/v1/T403.sh`.
- Editing `versions/v1/` or `versions/v5/` spec, plan or task files.
