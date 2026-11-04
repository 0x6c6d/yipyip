# Prompt: Generate the tasks of one version

> Input: `.harness/versions/vN/plan.md` and `.harness/Plan.md` (+ the spec of the version for details the plan
> summarized: `.harness/harness.md` for v1, `.harness/versions/vN/spec.md` for N ≥ 2). Output: `.harness/versions/vN/Tasks.md`
> (a small index), `.harness/versions/vN/tasks/<ID>.md` (one brief per task) and `.harness/verify/vN/<ID>.sh`
> (one executable verification per task). Together they are the state the supervise loop executes
> task by task. Run it with: "Follow .harness/prompts/generate-tasks.md for version vN."

---

## Role

You are the planner who wrote the version plan, now the **task decomposer**. Your output is read by a loop,
not a human: every task must be small, self-contained and checkable, so that a fresh sub-agent
with zero history and no knowledge of the plans can do it correctly from its task file alone.

## Why three kinds of files

The loop re-reads the state file every iteration. A state file that carries every brief and
every verification command grows to hundreds of kilobytes and is paid for on every iteration.
So: `Tasks.md` holds only what changes (status, notes) and what selects (dependencies, tier); the
brief is read once, when the task is dispatched; the verification is a script, so a pipe or a
quote in it can never be damaged by table escaping, and "exit 0" is the whole contract.

## Reasoning process (do this before writing)

1. **Decompose.** Split every phase into atomic tasks: one independently verifiable outcome each
   (one module, one endpoint, one migration, one integration call). If the description needs
   "and" for two outcomes, split it. Size guard: about one sub-agent session, roughly at most
   5 files and 200 changed lines, and a brief of at most about 6000 characters; bigger → split.
   A task that must also adapt existing tests because a registration changes global behaviour is
   a smell: isolate the global state once (a fixture) instead of touching old tests in every task.
   Expect 3–8 tasks per phase. Phase 0 (Preflight) becomes task T000 with the preflight commands
   as its verification; it is run by the supervisor itself, not a worker. The last phase ends
   with two documentation tasks. (a) **README task** (always `sonnet`, a fixed tier the planner
   never raises or lowers): create (version 1) or update (version N) the repo-root `README.md`: a
   professional, clean, public-facing project README in the style of well-maintained open-source
   projects, not a build log. It is written for someone who has never seen the harness and must
   not mention `.harness/`, the loop, plans or tasks. Required sections in this order: title with
   a one-line description; a short "what and why" (3–5 sentences) and a feature list; Requirements
   (runtimes, tools, versions); Installation (copy-pastable commands, from clone to working);
   Quick start / Usage (a minimal runnable example, then the common commands or API calls, with
   expected output where short); Configuration / Customization (only if the project has
   user-facing options AND the agent can read them clearly from the code or spec: a table with
   name, default, effect and where it is set (file, env var or flag), plus one example config;
   list only options it actually understood, never guess, and omit the whole section if there are
   none or they are unclear); Project structure (short tree); Development (how to run tests and
   the `check` command); License only if the spec names one, never invent one. Rules: take every
   command, option and default from the actual code and spec, never from memory; no marketing claims,
   no invented badges, no placeholder text (no `<...>` stand-ins), no TODO; if the repo has no remote (`git remote get-url origin`
   fails) never invent a clone URL: write "Clone this repository" in words; short headings, code blocks with language
   tags, one idea per paragraph. Version N updates only the sections its changes touch (new
   options, commands or behaviour) and keeps the rest byte for byte. Its verification script
   checks, kept light: the file exists, the core headings are present (`grep -E '^## '`; the
   Configuration heading only if the README has one), no `.harness/`, `TODO`, `TBD` or `<...>` placeholder text, and
   the Installation and Quick start commands run in a clean temp checkout (wrapped in
   `timeout`). It does not try to prove that every config option is listed. (b) **Docs task**: update `CLAUDE.md` build/test/lint
   commands and architecture notes (keep its "Delegation workflow" section unchanged) and add
   deeper docs under `docs/` only if the spec asks for them. A Plan assumption that freezes
   `CLAUDE.md` overrides the last rule; a spec that protects only a section of a file protects
   only that section. `README.md` depends on every task that adds user-facing behaviour or
   config, so it documents the final state. (c) The list always ends with the **Definition-of-Done row** (`Tier` =
   `supervisor`, depends on every other row): its script runs the Global Definition of Done of the
   version plan, including the verification script of the README task; a version without this row
   is incomplete.
2. **Order and dependencies.** Flat top-to-bottom list; record the IDs each task needs `done`
   first, and only those it really needs (shared helpers, fixtures, interfaces it calls), never
   "the previous task in the same phase": a blocked task must not block independent ones. The
   loop scans linearly and skips rows whose dependencies aren't `done`.
3. **Model tier per task** (not per phase) using .harness/Plan.md's routing policy. Any task touching a
   Consequential assumption or a Risk item is pinned to `opus`, however small. Precedence: a
   security rule beats a cost example in the routing policy. Format: `<tier>`; the one-clause
   reason goes into the task file.
4. **Verification per task** (the most important part): `.harness/verify/vN/<ID>.sh`, built from the
   shared `.harness/verify/_lib.sh` (see *Verification scripts*). Something the supervisor can run
   and get pass/fail from (a command, a named test, a request + expected response, a file with
   given content). Never "review the code". It must end on its own: wrap anything that could
   hang in `timeout`, and start and stop servers inside the script. `opus` tasks also need at
   least one negative check (invalid input, unauthorized access, malformed data) in the script.
   Prefer one `-e` per pattern over a regex alternation, and never rely on a pipe inside a
   Markdown table cell: scripts have no such problem.
5. **Self-containment.** Each task file names the files to create/change, the interfaces to
   follow and the constraints, with no "as before" or "see T004". Tests are part of the task
   that creates the code, not a later task. Constraints that hold for every task (protected
   paths, no commits, no network, the check command) are written once in the *Standing
   constraints* section of `Tasks.md` and are not repeated in task files.
6. **Coverage check (last).** Every phase has tasks; every core requirement **of this version's
   spec (every new or changed R-number and acceptance criterion, not only the plan's summary)**
   appears in at least one task file (its `Covers:` line) or in the version plan's *Requirement
   coverage* table; every Risk item has a task with the extra scrutiny from 3–4. Fix gaps now, in
   the version plan as well when it missed one.

## Resuming an existing Tasks.md

If `.harness/versions/vN/Tasks.md` exists, update it, don't regenerate (never touch the files of other
versions): keep `Status`/`Notes` of unchanged task IDs
(never reset `done` or `blocked`), insert new tasks as `todo` at the right position, mark tasks
that no longer apply as `superseded` with a one-line reason, and add a Changelog entry. When more
than about 15 tasks are new, generate them in per-phase batches and commit each batch.

## Output format

### `.harness/versions/vN/Tasks.md` (the index; stays small)

[template begin]
# Tasks v<N>: <product name>

_Generated from .harness/versions/vN/plan.md. This file is the loop's persistent state (status and notes
only): update it in place, never regenerate it during a build. Briefs live in `.harness/versions/vN/tasks/<ID>.md`,
verifications in `.harness/verify/vN/<ID>.sh`. IDs are unique within this version._

## Standing constraints

The supervisor prepends these, verbatim, to every worker brief:

- <check command> must pass for the whole repo.
- Protected (never modify): <list, including the whole .harness/ folder and .claude/; not the
  repo-root README.md, which is not protected: the scope rule "nothing outside the task" keeps other
  tasks away from it>.
- <no commits; network/live-system/tool rules that hold for every task>

## Task List

| ID | Phase | Title | Depends-on | Model | Status | Notes |
|----|-------|-------|------------|-------|--------|-------|
| T000 | 0 | Preflight | — | supervisor | todo | |
| T003 | 1 | Config loader | T002 | haiku | todo | |

Status values: `todo` · `done` · `blocked` · `superseded`. No pipe characters inside cells.

## Execution Protocol

Run with the `loop-coding` skill (`/loop-coding vN`). The authoritative procedure
(round limit, commit format, Definition-of-Done repair) is in
`.claude/skills/loop-coding/SKILL.md`; in short, per iteration:
1. Select the first `todo` row whose dependencies are all `done`.
2. Read `.harness/versions/vN/tasks/<ID>.md`; dispatch one fresh worker with the row's `Model` and a brief made of the
   Standing constraints, the task's Brief and the path of its verification script.
3. The supervisor runs `.harness/verify/vN/<ID>.sh` itself (incl. its negative checks); never trust
   the worker's claim.
4. Pass → one commit with the work and this file's update. Fail → feedback; still failing
   after 3 rounds → `blocked`.
5. Nothing selectable → run the Global Definition of Done of `.harness/versions/vN/plan.md`; done when it passes.

## Changelog
<omit on first generation>
[template end]

### `.harness/versions/vN/tasks/<ID>.md` (one per task)

[template begin]
# <ID>: <title>

Phase <n> · Model: <tier>: <one-clause reason> · Depends-on: <IDs> · Covers: <R-numbers>

## Brief
<files to create/change, interfaces, constraints, tests to write; self-contained>

## Verification
`.harness/verify/vN/<ID>.sh` exits 0. <One or two sentences saying what it checks, including the
negative cases.>
[template end]

Supervisor rows (`T000`, the final Definition-of-Done row) also get a task file and a script.

### `.harness/verify/vN/<ID>.sh` and `.harness/verify/_lib.sh`

`.harness/verify/_lib.sh` is shared by all versions and written once (when it is missing) and provides `expect_ok <label>` and
`expect_fail <label>`, which read one command from stdin (use a quoted heredoc, so nothing is
expanded or escaped), run it with `bash -c` from the repository root under a `timeout`, and
count a failure when a positive command does not exit 0 or a negative one does not exit with an
ordinary failure code (a timeout or a missing command is not an expected failure); `finish`
prints the result and exits non-zero if anything failed. Each script is:

[template begin]
#!/usr/bin/env bash
# Verification for <ID> (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "<label>" <<'CMD'
<command>
CMD
expect_fail "negative: <label>" <<'CMD'
<command that must fail>
CMD
finish
[template end]

## Output contract

- With file tools: write `.harness/versions/vN/Tasks.md`, `.harness/versions/vN/tasks/` and `.harness/verify/vN/` at the repo root (follow
  *Resuming an existing Tasks.md* if they exist; make scripts executable). Don't paste them into the chat; reply with one
  line: number of tasks per tier.
- Without file tools: output the files as fenced blocks, each preceded by its path, and nothing
  else.

## Constraints

- Every task is resolvable from its own task file plus the Standing constraints and excerpts
  attached at dispatch.
- Every verification script is mechanically runnable and terminates; if you can't write one,
  decompose further.
- Never downgrade a task tied to a Consequential assumption or Risk item away from `opus`.
