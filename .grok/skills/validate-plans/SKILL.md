---
name: validate-plans
description: >
  Compare each sandbox plan's Aim to the code. Intent in the code means the
  plan is completed: archive it to historical-plans whatever Status says.
  Intent absent and the plan will not be implemented: archive it to
  historical-plans/not-implemented. Live work stays in plans/. Use when the
  user says validate-plans, /validate-plans, archive validated plans, or
  check whether my plans were implemented.
when-to-use: "validate-plans, /validate-plans, archive validated plans, did my plans land, plan intent review"
user-invocable: true
---

# validate-plans

Review **this session’s Host+Worktree** sandbox plans still in `$SANDBOX/plans/`.
Planning-only (sandbox writes). Do **not** implement app source. Do **not** spawn.

Cite, do not paste: `AGENT_CONTEXT.md`, `AGENT_MANDATES.md` §2, `dedicated-planner.md`.

## Roles

- **Planner** and **orchestrator:** may run this skill.
- **Coder** and **master:** **refuse** unless the human explicitly says to run
  `/validate-plans` anyway.

## Identity

- **Host** = `git rev-parse --show-toplevel`
- **Worktree** = `AGENT_CONTEXT.md` **Agent ID**

Ignore a plan file unless both `Host:` and `Worktree:` in its header match.
Ignore `$SANDBOX/historical-plans/`, including `not-implemented/`.
Ignore basenames listed in `$SANDBOX/validate-plans-ignore/<Worktree>.txt`
(create the dir/file if needed; one basename per line). That list is **only
for this Worktree** — do not stamp a global skip on the plan file.

## Unstamped files

Plans in `$SANDBOX/plans/` missing `Host:` or `Worktree:`: **list in chat**.
Ask which to validate this turn, which to append to **this** ignore file,
which to stamp as this Host+Worktree (**only** if the human says it is theirs).
Do not guess.

## No cap

Process every matching file still in `plans/`.

## Completion

Compare the plan's **Aim** (intent) to the current code. The Critical Files table is a map of where to look. It is not the pass condition. The Status line is not the completion test.

1. Intent is in the code → the plan is completed. Stamp `Validated: YYYY-MM-DD Worktree=…` and `mv` to `$SANDBOX/historical-plans/`. Do this when Status says `DRAFT`, `APPROVED`, `BLOCKED`, `CODE LANDED`, or nothing. Implemented intent wins over a will-not-implement status.
2. Intent is missing or only partly in the current code. Before a gap plan or a pending report, check whether a later plan's implementation changed that code after this plan was implemented. Read later plans still in `$SANDBOX/plans/` and the git history of the code the Aim names. Do not read `$SANDBOX/historical-plans/`. That git history is only for this later-plan check. It is not the completion test in step 1.
   - A later change accounts for the difference → this is not a gap and it is not still-pending work. Do not write a gap plan that would undo the later plan. Stamp `Validated: YYYY-MM-DD Worktree=…` and `mv` to `$SANDBOX/historical-plans/`. Name the later plan or commit in chat.
   - You cannot tell "never implemented" from "implemented, then changed" → report that. Do not write a gap plan.
3. Intent is not in the code, no later plan explains that, and the Status line or a header sentence says this plan will not be implemented → stamp `Validated: YYYY-MM-DD Worktree=… not-implemented` and `mv` to `$SANDBOX/historical-plans/not-implemented/` (`mkdir -p` on first use). Do not ask. Status words: `SUPERSEDED`, `CANCELED`, `CANCELLED`, `ABANDONED`, `WITHDRAWN`, `REJECTED`, `OBSOLETE`, `DROPPED`. A body mention of obsolete docs does not use this step.
4. Intent is only partly in the code, and no later plan explains the missing part → report the gap and write a new DRAFT gap plan with this Host+Worktree. One plan if the delta is small; split if the land went badly wrong. The gap plan must not undo a later plan. Leave the source in `plans/` until the human accepts the gap plan or says archive anyway.
5. Intent is absent, no later plan explains that, and the plan is still live (`DRAFT`, `APPROVED`, `BLOCKED — needs replan`, or no will-not-implement status) → report **pending**. Leave the file.

Do **not** move plans to `historical-plans/` at CODE LANDED time. This skill archives after the intent-vs-code check.

## Forbidden

- App / tracked non-sandbox edits
- Guessing another Host or Worktree
- Treating recaps, compact summaries, build logs, `ENGINEERING_LOG.md`, or a `builds` tag as the completion test
- Treating the Status line as the completion test
- `resume_from` of execute children

## Output (chat)

Pending list; implemented archive list (`historical-plans/`); not-implemented archive list (`historical-plans/not-implemented/`); ignore-list additions; gap plan path(s).
