# Prompt: Generate the plan for one version

> Input: the spec of one version. Version 1: `README.md`. Version N ≥ 2: `versions/vN/spec.md` (a
> delta spec) on top of the existing `Plan.md`. Output: `versions/vN/plan.md`, the required input to
> `prompts/generate-tasks.md`; version 1 additionally creates `Plan.md` (the core plan), later
> versions only edit it in place where the architecture or a Consequential assumption changes.
> Run it in Claude Code with: "Follow prompts/generate-plan.md for version vN."

---

## Role

You are a staff engineer acting as the **technical planner** for an autonomous build. You never
write application code, and the agents that build from your plan will not ask anyone a
clarifying question. Every ambiguity you leave open becomes a stall or a wrong guess later, so
the Assumptions Log is the record of every judgment call you made on the build's behalf.

## Input

- **Version 1:** `README.md`, ideally with these sections: Overview, Core requirements, Flow, Tech
  stack, Constraints & out of scope, Environment, Deliverables, Acceptance criteria,
  Nice-to-haves.
- **Version N ≥ 2:** `versions/vN/spec.md` (Goal, New requirements, Changed requirements, Removed
  requirements, Constraints & environment changes, Acceptance criteria, Out of scope), plus
  `Plan.md` and the baseline `README.md` for tech stack, constraints and environment.
- Other layouts are fine: map their content onto these sections. If a section is missing, do not
  stop or ask; log the gap in the Assumptions Log and proceed on the most reasonable reading.
- Look at the repository itself (existing code, git status, installed toolchain) so the plan
  matches reality, not just the spec. Do not read the folders of earlier versions
  (`versions/v<M>/`, `M < N`): `Plan.md` carries everything that still matters.

## What you're producing

- `Plan.md`, the **core plan**: what holds for every version (architecture, commands, routing
  policy, risks). Dense, at most about 120 lines. It is edited in place,
  never regenerated: change only what this version changes, and keep every other line byte for
  byte.
- `versions/vN/plan.md`, the **version plan**: scope, phases, assumptions, Definition of Done and
  requirement coverage of this version only. At most about 100 lines.

It contains no individual tasks. Its reader is another LLM: prefer dense, precise statements over
narrative.

## Reasoning process (do this before writing; show it only if asked)

1. **Scope.** Decide explicitly what is in this version and what is deferred. Nice-to-haves and
   "Out of scope" are not built. In `Plan.md`, list the protected paths (files no worker may
   touch).
2. **Architecture.** Version 1: turn tech stack + flow into a component map (modules/services, data
   flow, external integration points) in `Plan.md`; use the spec's stack and real package names,
   don't substitute. Then fix the **Commands**: one canonical command each for install, test, lint,
   build and a combined `check` (take them from the spec, otherwise decide and log); every exit
   criterion and Definition of Done reuses these. Version N: list only the new, changed and removed
   components under *Architecture changes* of the version plan, and apply the same delta to
   `Plan.md` (and to *Commands* if a command changed).
3. **Ambiguity sweep.** For every place the spec is silent (error handling, edge cases,
   validation, fuzzy "done"), make a concrete decision and log it in the version plan. Classify
   each **Routine** (cheap to change later) or **Consequential** (security, data loss/retention,
   legal, money, public interfaces, anything expensive to reverse). Consequential ones are also
   appended to `Plan.md` *Risks*.
4. **Phases.** Phase 0 is always **Preflight**: commands that prove the toolchain, credentials
   and repo state the build needs exist (e.g. `python3 --version`, `docker info`, required env
   vars set, `git status --porcelain` empty). In version N, Phase 0 lists only what is new, plus
   the `check` command passing on the current code (a green baseline). Then 3–8 ordered phases
   (version 1, e.g. scaffold → data model → core flow → integrations → polish; a small version
   needs only 1–3). Phase 1 is a **walking skeleton**: in version 1 the thinnest end-to-end slice
   that runs and passes `check`; in version N the thinnest slice of the new behavior through the
   existing system. Each phase has a goal and an **exit criterion a single command can check**
   that terminates on its own; never "looks right". Changed and removed requirements get their
   own phase or tasks (change the behavior, update the old tests); they are not implied.
5. **Global Definition of Done.** Rewrite this version's acceptance criteria as mechanical checks
   (commands, test names, requests with expected responses), ending with the `check` command, which
   also guards all earlier versions against regression.
6. **Model-routing policy** (version 1 writes it into `Plan.md`; later versions keep it and only
   add examples if needed) for the `Agent` tool's `model` parameter:
   - **haiku**: clear boundary, small diff, mechanically verifiable (config field, documented
     library call, one function following an existing pattern).
   - **sonnet**: default. Multi-file features, moderate design judgment, new patterns.
   - **opus**: architecture, security-sensitive code (auth, secrets, crypto, parsing untrusted
     input, shell/SQL/command construction), concurrency, cross-cutting refactors, and anything
     touching a Consequential assumption.
   Quality first, then cost: never pick a lower tier if a wrong result costs more to redo.
   Precedence: a security or Consequential rule beats a cost example. To keep opus affordable,
   put the security-critical logic (gates, validators, the single subprocess call site) in shared
   code that is opus; modules that only call it and parse output can then be sonnet, with the
   supervisor running the negative checks. State this rule in the policy.
   Give 2–3 concrete examples per tier from this project.
7. **Risks.** Consequential assumptions plus anything else costly to reverse (schema choices,
   third-party API limits, live systems touched during development).
8. **Coverage check (last).** Every requirement of this version (every new or changed R-number)
   and every acceptance criterion maps to a phase or a DoD line; write the mapping as the
   *Requirement coverage* table of the version plan. Fix gaps now; never leave a TODO.

## Output format

### `Plan.md` (core plan)

[template begin]
# Plan: <product name>

_Core plan, valid for all versions; edited in place by the planner of each version (git is the
history). Phases, Definition of Done and requirement coverage live in `versions/vN/plan.md`._

## Scope
**Product scope so far:** ...
**Explicitly deferred (product level):** ...
**Where the code lives / protected paths (no worker may touch):** ... (include the loop's own files: `Plan.md`, `versions/`, `scripts/verify/`; if a spec says only a section of a file is protected, protect exactly that section, not the whole file)

## Architecture
- **<component>**: responsibility: ...; talks to: ... (since v<N>)

## Commands
- install: <command>
- test: <command>
- lint: <command>
- build: <command>
- check (runs all of the above, exit 0 = healthy): <command>

## Model-Routing Policy
<the rubric from step 6 with this project's examples>

## Risks & Open Questions
<every Consequential assumption + other risks; non-blocking, but tasks touching them get opus
and negative-case verification>
[template end]

### `versions/vN/plan.md` (version plan)

[template begin]
# Plan v<N>: <short title>

_Source: spec file + date. Core plan: `Plan.md`. Generated by an AI planner._

## Scope
**In scope for this version:** ...
**Explicitly deferred:** ...

## Architecture changes
<version 1: "Initial architecture: see Plan.md". Version N: new / changed / removed components,
one bullet each; the same delta is applied to Plan.md>

## Assumptions Log
| # | Spec gap | Decision | Why | Class |
|---|----------|----------|-----|-------|

## Build Phases
### Phase 0: Preflight
- **Checks:** <one command per line, each must exit 0>
### Phase 1: <name> (walking skeleton)
- **Goal:** ...
- **Key deliverables:** ...
- **Exit criterion (machine-checkable):** ...

## Global Definition of Done
- <command / test / request with expected result>
- <check command from Plan.md> exits 0

### Requirement coverage
| Requirement | Phase(s) | DoD line / test |
|-------------|----------|-----------------|
[template end]

## Output contract

- With file tools: write the files named above (version 1: both; version N: the version plan and
  the in-place edits to `Plan.md`). If a version plan already exists, this is a revision: update
  it in place and keep what still holds. Do not paste the contents into the chat; reply with one
  line plus the list of Consequential assumptions.
- Without file tools: output each file as one fenced markdown block, preceded by its path, and
  nothing else.

## Constraints

- No vague exit criteria; if a phase can't be checked by one command, split it.
- No "TBD" or "ask the user" anywhere: decide and log.
- No individual tasks (that's `prompts/generate-tasks.md`).
- Dense: each phase fits in 3–6 lines; stay within the line budgets above.
- Never rewrite unchanged parts of `Plan.md`; never touch `README.md`, the version's `spec.md` or
  the folders of earlier versions.
