# Prompt: Write the delta spec of a new version

> Input: an idea for the next version of an existing project. Output: `.harness/versions/vN/spec.md`, a
> small delta spec that the planner reads on top of `.harness/harness.md` (the baseline) and `.harness/Plan.md`.
> Run it in Claude Code with: "Follow .harness/prompts/new-version.md. Idea: [describe the new version]"

---

## Role

You are a senior product engineer writing the spec of one increment for an autonomous coding loop.
The loop never asks questions. Whatever the spec leaves open becomes a guess, so be concrete, but
write only what is new: everything unchanged is already described by `.harness/harness.md` and `.harness/Plan.md`.

## Steps

1. **Pick N.** v1 is `.harness/harness.md`. N is one more than the highest existing `.harness/versions/v<M>/` folder
   (v2 for the first new version). Find the next requirement number with
   `grep -ohE '\bR[0-9]+\b' .harness/harness.md .harness/versions/*/spec.md | sort -V | tail -1`: numbering continues
   across versions (example only: if v1 ended at R20, v2 starts at R21; use the number the command prints).
2. **Read** `.harness/harness.md` (baseline spec), `.harness/Plan.md` (core plan) and the repository itself. Do not
   read the other folders under `.harness/versions/`, except the `grep` of step 1 for the highest R-number.
3. **Size check.** If the idea is one small change that a single verification command can check,
   say so, suggest `/loop-coding <task + how to recognize "done">` instead, and stop.
4. **Interview me:** short questions, one at a time (at most 6). With every question, propose a
   default I can accept with "ok". Cover: the goal of this version, what is new, what changes in
   existing behavior, what gets removed, new tools / packages / credentials / live systems, and
   how "done" is checked. Look at the repository yourself instead of asking what you can see.
5. **Write `.harness/versions/vN/spec.md`** (create the folder) in exactly these sections and this order:
   1. Goal: what and why, 2–3 sentences
   2. New requirements: numbered from the next free R-number; each one testable
   3. Changed requirements: `R3: was <old behavior> → now <new behavior>` (existing numbers);
      "none" if there are none
   4. Removed requirements: `R7: <what to delete>`; "none" if there are none
   5. Constraints & environment changes: only deviations from sections 4–6 of `.harness/harness.md` (new
      packages with versions, new tools each with a check command that exits 0, new secret env
      var names, new live systems the build may touch); "none" if there are none
   6. Acceptance criteria: checkboxes, each verifiable by a command or test with an expected
      result and naming the requirement(s) it covers (e.g. "[R21]"); every new and changed
      requirement is covered at least once; one criterion is that the `check` command from
      `.harness/Plan.md` still exits 0 (earlier versions keep working)
   7. Out of scope: explicitly not part of this version

## Rules

- Short: at most about 80 lines. Do not restate unchanged requirements, flow or tech stack.
- If the version adds or changes anything a user sees (commands, options, defaults, install steps),
  add one requirement that the repo-root `README.md` documents it (install, usage, and a
  configuration table only if there are clear options), with an acceptance criterion a command can
  check. The README is a project file, not
  part of `.harness/`.
- No vague words ("fast", "user-friendly") without a measurable number; mark your own assumptions
  with "(assumption)"; write in English.
- Write only `.harness/versions/vN/spec.md`: no code, no plan, no tasks, no edits to `.harness/harness.md`.
- Finish with the list of your assumptions for me to confirm; after I confirm, commit the file as
  "vN spec" and tell me to start `/loop-coding vN`.
