# Plan Style Guide

Reference for planners. Full rules: `AGENT_MANDATES.md`.

## Filename

```text
<descriptive-kebab>-YYYYMMDD-HHMM-plan.md
```

- **Minutes** required when stamping (`YYYYMMDD-HHMM`).
- **Not** day-only. Seconds not required.
- New contract (cleanup / replan) → **new file** + new minutes.

## Status header

| Status | Who |
|--------|-----|
| DRAFT | Planner while drafting |
| APPROVED | Coder at execute start |
| CODE LANDED | Coder at successful handoff |
| BLOCKED — needs replan | Coder if completeness cannot finish |
| DONE / SUPERSEDED | Optional after intent satisfied |
| Validated | `/validate-plans` then `mv` to `historical-plans/` |

Stay in `plans/` until David runs `/validate-plans` (including CODE LANDED). Do not archive at land.

Track progress with what already exists: `./orch/plans-scope` (plans, Status, commits since), `plans/` versus `historical-plans/`, `./orch/todo-open [filter]` (open TODO items), `./orch/englog-recent` (log headers; `--show '<header>'` for one entry), and `git log`. Never read `ENGINEERING_LOG.md` or `TODO.md` whole. Do not create separate status or tracking files.

Required header on **new** plans:

- `Host:` git toplevel
- `Worktree:` `AGENT_CONTEXT` Agent ID
- `Aim:` 2–5 lines of intent (not Critical Files)

Do not nag humans about stale DRAFT after they ordered execute — hygiene-fix or ignore when eng-log/git show execution.

## Scope of one plan

- One plan per file set or behavior area. Put all pending related work (several roadmap items, a cleanup series, follow-ups in the same files) into that one plan, as ordered phases.
- Split only when the file sets do not overlap, when one item's result changes another item's design, or when the combined change is too big to finish and build green in one go.
- Do not write a successor plan whose files overlap a plan that has not landed. Write it after that plan lands, from the current tree.

## Structure

Context · Recommended Approach · Critical Files (map) · Existing utilities · **Phases** (order of work) · **Acceptance** (the single checklist).

**Compliance:** one line citing `standard-plan-compliance-block.md` — **do not paste**.

## Do

- State each change once, in Acceptance. Acceptance is the one checklist the coder and reviewer use; Aim and Phases do not repeat it.
- One line `Review: diff` when Acceptance is behavior a diff can hide (state, migration, merge, persistence). A deletion, a doc edit, or a pin bump omits it.
- Describe the problem and the fix: the predicate ("encoder writes every key; a missing tlsMode stays None"). A short code fragment is fine where it states the problem or the fix more clearly than English. The coder designs and writes the change. A plan that hands the coder text to paste in is overspecified.
- Long forensics → `sandbox/research/<slug>.md` + link (or chat).
- **Finish greps in planning.** Never leave `unless` / `grep X` / “if that path walks…” in a plan about to be approved. Write the answer (`Kotlin: no edits.` or the exact `fun`).
- **Critical Files is a map**, not a fence. It says where to start, as path + function or symbol. For a large file, add the line range when you have it from the current branch tip; never copy line numbers from an older note or plan. List known callers and tests when you already found them, to save the coder a search. The Aim, not this list, sets what may change.
- **Named functions**, not “energy path.” Copy the skip/guard predicate once.
- **Fewest phases.** Phases order the work; they are not commit or build points. One file → usually one phase. Docs last. Do not add phases so you hit ~3–8.
- **Self-contained.** Executor re-reads this file + role pack only. Do not require memory of the last CODE LANDED plan. One sentence of prior-cycle context is enough. Behavior that must stay true lives in the project's spec and policy docs and tests (project-facts.md points to them); cite those by path instead of restating them.

## Shared values

For each value the plan writes, list every other path that writes or reads it. Include drafts, an open editor, and a load that started before the write. For each path, state the end result. The reviewer checks this list on the first pass.

## Callers and tests

Callers, routes, `NavHost` arguments, and tests of a changed name are part of the change, whether or not the plan lists them; the coder updates them. When you already know them, list them under Critical Files so the coder does not repeat the search.

## Engineering log

`ENGINEERING_LOG.md` only grows. The commit file list includes it. The edit is one new line at the end, added with `./orch/append-to-engineering-log`. Do not replace, delete, or rewrite an existing line, and do not reset the file. A plan does not require a log line and also forbid a commit, or require a clean tree that the log line would dirty. A pin bump does not add a log line.

## Do not

- Mandate Acknowledgment section
- Paste STANDARD BLOCK / AGENT_MANDATES
- Ultra-micro / max-granularity language (except post–inning recovery). Exact function names in Critical Files are **not** ultra-micro.
- Research assignments in the execute contract (`grep expandDiagnoseMany`, “unless a native-null path lacks it”)
- Optional extra sites (“and oriented energy if it walks a seed that small”)
- Multi-plan LANDED audit tables inside the next feature plan
- Device forensics novels in the execution contract
- Commit or build steps inside phases (the STANDARD BLOCK commits and builds once, at the end)
- Restating landed behavior or "keep" rules from earlier plans (cite the spec, policy doc, or test by path)
- Code for the coder to paste in, or transcripts of current code
- The same change stated in Aim, Phases, and Acceptance
- `./build_app @file` syntax in plan text
- Success lines that require discovering paths (“every native-null fallback”)
- A log line paired with "no commit" or "clean tree"
- Replacing, deleting, or resetting `ENGINEERING_LOG.md`

## Good target style

Short exact plans (e.g. one-weight UI tweak) or lock tables with clear Acceptance — not process essays.

## TODO-linked plans and research

- **Open** (`- [ ]`) TODO items that name a plan/research path: keep that file under `plans/` or `research/` — do **not** archive while the item is open.
- **Closed** (`- [x]`) items, or plans that are **obsolete** (done and superseded): archive the plan if nothing **open** still points at it; close/eliminate the TODO bullet if the work is obsolete (do not leave open TODOs aimed at dead plans).
- Prefer: close obsolete TODO first, then archive the plan.
