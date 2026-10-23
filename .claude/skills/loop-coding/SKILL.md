---
name: loop-coding
description: "Supervisor/worker loop for coding work: one worker sub-agent per task (model tier chosen per task), the supervisor verifies the real result itself (tests/build/diff, never the worker's self-report), re-prompts with concrete feedback, escalates a task that keeps failing to a stronger model and blocks it if that fails too, and stops when a checkable definition-of-done holds. Works for a single task or for versions of a project (v1 from README.md, later versions from versions/vN/spec.md), each driven by spec → plan → Tasks.md. Use when the user asks to delegate or supervise work, build a project or a new version from a spec, run a Tasks.md, or invokes /loop-coding."
---

# Supervise loop

The supervisor (this session) briefs, verifies, records and commits. It never writes product
code itself; workers do. Args: a task description, or a version `vN`, or nothing (= version mode for
the newest version, i.e. the highest `versions/vN`, or `v1` if there is none).

Terms. **Round**: one worker attempt (first dispatch or one feedback message) plus your
verification. **Protected paths**: everything the spec's *Constraints* marks untouchable, plus
`README.md`, `Plan.md`, `versions/`, `scripts/verify/`,
`prompts/`, `.claude/` and the "Delegation
workflow" section of `CLAUDE.md`. Workers never edit protected paths (exception: the generator
workers of step 0 write `Plan.md` and the files of the active version); only you edit the
version's `Tasks.md` (status and notes). Documentation therefore goes under `docs/`, never into `README.md`.
**Versions**: the build is split into versions. Version `vN` lives in `versions/vN/`; its spec is
`README.md` (v1) or `versions/vN/spec.md` (a delta spec, N ≥ 2); its plan is `versions/vN/plan.md`
(phases, assumptions, Definition of Done, coverage); `Plan.md` is the stable core plan
(architecture, commands, routing policy). Work only on the active version; never read the folders
of other versions.
**Task files**: `versions/vN/Tasks.md` is only a small index (ID, phase, title, dependencies,
tier, status, notes) plus the *Standing constraints*; the full brief of a task is
`versions/vN/tasks/<ID>.md` and its verification is the executable `scripts/verify/vN/<ID>.sh`
(exit 0 = pass). Read the index every iteration, read a task file only when you dispatch or
repair that task, never the whole `tasks/` directory.
**Check command**: the project-wide command from Plan.md *Commands* (tests + lint + build); in
single-task mode the project's own test command (see `CLAUDE.md`, else discover it).

## 0. Choose the mode

- **Single task** (bug, feature, refactor): go to step 1 with that task.
- **Version** (args `vN`, empty, or "build the project"; empty = the newest version):
  1. `README.md` missing or `grep -q SPEC-PLACEHOLDER README.md` succeeds → stop and tell the
     user to create the spec first (the placeholder contains the prompt). For N ≥ 2,
     `versions/vN/spec.md` missing → stop and tell the user to run `prompts/new-version.md`.
  2. No `versions/vN/plan.md` → dispatch one `opus` worker: "Follow `prompts/generate-plan.md` for
     version vN." Verify yourself: the version plan has all five sections
     (`grep -cE '^## (Scope|Architecture changes|Assumptions Log|Build Phases|Global Definition of Done)' versions/vN/plan.md`
     prints 5) and `Plan.md` has all five core sections
     (`grep -cE '^## (Scope|Architecture|Commands|Model-Routing Policy|Risks)' Plan.md`
     prints 5), Phase 0 Preflight exists, `grep -inE 'TBD|ask the user' Plan.md versions/vN/plan.md`
     is empty, `wc -l` stays within the budgets (Plan.md about 120 lines, version plan about 100;
     way over → send it back to condense), every acceptance criterion maps to a phase or DoD line
     (read, don't grep). For N ≥ 2 also check that `git diff Plan.md` touches only architecture,
     commands and risks. Commit `vN plan`.
  3. No `versions/vN/Tasks.md` → one `opus` worker with `prompts/generate-tasks.md`: "Follow
     `prompts/generate-tasks.md` for version vN."; with more than ~15 tasks to generate, one
     dispatch per phase batch, each committed (`vN tasks: batch n`). Verify: every phase has tasks,
     every row has a tier and a `versions/vN/tasks/<ID>.md`, every worker row has an executable
     `scripts/verify/vN/<ID>.sh` that passes `bash -n` and contains a negative check for opus
     rows, the index is well-formed
     (`awk -F'|' '/^\| T/{print NF}' versions/vN/Tasks.md | sort -u` prints exactly one number; no
     pipes inside cells), and every new or changed R-number of the version's spec appears in a
     task file's `Covers:` line or in the version plan's coverage table. Commit `vN tasks: initial`.
  4. **Checkpoint:** show the user the Consequential assumptions (Class column of the version
     plan's Assumptions Log) and the tasks per tier; continue
     after their OK. Skip only if the user said to run unattended.
  5. Run `versions/vN/Tasks.md`: do step 1 once (it is row `T000`, `Model` = `supervisor`: you run its
     verification yourself, no worker; then mark it `done` and commit) and step 3 once (arm
     `/goal`). Then repeat steps 4–6 per row: always the first `todo` row whose `Depends-on` are
     all `done`, one row at a time. Step 2 is already the row's verification script.
     `versions/vN/Tasks.md` is the state: after a restart, continue from its statuses.

## 1. Preflight

Run the toolchain checks yourself (Phase 0 of the version plan, or the obvious ones: runtime, package
manager, required external tools, env vars). If something is missing, stop and give
the user the exact install command; workers can't fix a missing toolchain and burn tokens trying.
`git status --porcelain` must be empty before every worker starts; if not, stop and ask the user
to commit or stash (never start a worker on a dirty tree).

## 2. Define done

Single-task mode only; in version mode each row's verification script is the definition.
Exact pass/fail commands (tests, build, lint, a request + expected output), never "looks right".
Each must terminate on its own (`timeout`, no foreground servers). Where practical, see it fail
before the work starts, so a pass means something. If it's ambiguous and nothing in the repo or
conversation resolves it, ask once; otherwise decide and state the assumption in the final report.

## 3. Keep the loop alive

Arm `/goal` with the done condition (version mode: "every row in versions/vN/Tasks.md is done, superseded or
blocked-with-nothing-selectable, and the Global Definition of Done of versions/vN/plan.md passes"). It keeps the session
working across turns; stop it with `/goal clear`. Don't build your own retry logic on top.

## 4. Dispatch one worker

`Agent` with `subagent_type: general-purpose` (never `fork`), a clear `description`, and `model`:
- `haiku`: small, bounded, mechanically checkable
- `sonnet`: default
- `opus`: architecture; security-sensitive code (auth, secrets, untrusted input, shell/SQL
  construction); concurrency; anything tied to a Consequential assumption

**Base and escalation model.** The row's tier is its *base model*; the *escalation model* is the next
rung of the ladder `haiku → sonnet → opus` (opus has none). A row starts on its base model and is
escalated only by step 6 after consecutive failed rounds, never up front and never above the
ladder. An opus row therefore never escalates.

**Red first:** run the row's `scripts/verify/vN/<ID>.sh` once before dispatching. It must fail,
and for the expected reason (the feature is missing, not a typo in the script). If it already
passes, the row is vacuous or already satisfied: check which, then fix the row or close it
(`done`, note `already satisfied`); never dispatch blindly.

The brief is self-contained (the worker has no memory of this conversation): the *Standing
constraints* from `versions/vN/Tasks.md` (prepend them verbatim), then the *Brief* of
`versions/vN/tasks/<ID>.md` (goal, files to read and change, interfaces), the verification command
`scripts/verify/vN/<ID>.sh`, and only the Plan/spec excerpts it needs, not whole documents. It also states: use only the live systems the
spec's Environment allows, never any other host; never write secrets into files; install nothing
globally (report a missing tool instead). End every brief with:
"Run the verification yourself before reporting. Do not commit. Report in at most 10 lines:
files changed, commands run, result."

## 5. Verify yourself

- `git add -A`, then `git diff --cached --stat`; read only the hunks that matter. Staging makes
  new files visible to every check below (plain `git diff` ignores untracked files).
- Scope: `git diff --cached --name-only` contains no protected path and nothing outside the task;
  no tests deleted, skipped or weakened to get green; no secrets or `.env` files in the diff
  (`git diff --cached | grep -niE 'secret|token|passw|api[_-]?key'`, read the hits).
- Run `scripts/verify/vN/<ID>.sh` yourself (it prints one line per check and the tail of a failing
  one).
- Check point by point, including negative cases for opus tasks.
- Regression: run the check command, so a pass doesn't break earlier work. If it takes more than
  a couple of minutes, run it at phase ends and before the Global Definition of Done instead; it is
  also what protects earlier versions.

## 6. Pass, fail, stuck

Every change to `Tasks.md` or to a row's files is committed right away (`vN <ID>: <what>`): the
tree must be clean again before the next worker starts.

- **Pass:** one commit with the task's files and the `versions/vN/Tasks.md` update (status
  `done`, note: files + one line; no hash, it isn't known yet). Message `vN <task id>: <summary>`.
  Next task.
- **Fail:** `SendMessage` to the same worker: the failing command, the relevant output lines,
  what to change. Never just "try again".
- **Escalate after 2 consecutive failed rounds** on the base model (verification still red after the
  first dispatch and one feedback message): discard the worker's changes
  (`git restore --staged --worktree . && git clean -fd`, same safety as below), dispatch a **fresh**
  worker on the escalation model with the same brief plus the failing command, its output lines and
  one line of what the previous worker tried (never its transcript), and note
  `escalated to <model> after 2 failed rounds` in the row. The new worker starts with its own round
  count at 0. A row escalates at most once; a row already on opus is not escalated.
- **Still failing after 3 rounds on the last model** (the escalation model, or the base model when it
  cannot escalate): set the row `blocked` with what blocks it and what was tried
  (this note survives a restart), discard the task's changes
  (`git restore --staged --worktree . && git clean -fd`; the tree was clean before the worker
  started, so nothing else is lost), commit the `Tasks.md` update, and continue with every row
  that doesn't depend on a blocked one. When nothing selectable remains and something is
  blocked → stop and report. During task execution this is the only point where
  the loop waits for the user (besides the checkpoint, preflight and a missing spec).
- **Broken row** (the verification script itself is wrong or can't terminate, or the task
  contradicts the spec): fix `versions/vN/tasks/<ID>.md` or `scripts/verify/vN/<ID>.sh`, note `row fixed: <why>`
  in the index; this isn't a round. If fixing
  means deviating from the spec or a Consequential assumption → `blocked`.
- **Version done:** nothing selectable and nothing blocked → run the Global Definition of Done
  of `versions/vN/plan.md`. A failing item becomes a new fix row (`todo`, tier by the usual rules;
  new `versions/vN/tasks/<ID>.md` and `scripts/verify/vN/<ID>.sh` = that failing check) and the loop continues. After 2 such repair passes, stop and report the rest.
  Final report: what was built, the assumptions made, the commit range, the DoD output.

## Token rules

- Worker transcripts stay out of this session: read the final report, verify with targeted
  commands, never read whole transcripts or dump large files. Per iteration read the active
  version's `Tasks.md` index and one task file, nothing more; never the folders of other versions.
- One task per worker; the right tier per task (quality first, but no opus for mechanical work).
- Workers run one after another, never in parallel: the scope check via the staged diff and one commit
  per task rely on a tree that only one worker touches.
