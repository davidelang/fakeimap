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

Stay in `plans/` until `/validate-plans` (including CODE LANDED). Do not archive at land.

Required header on **new** plans:

- `Host:` git toplevel
- `Worktree:` `AGENT_CONTEXT` Agent ID
- `Aim:` 2–5 lines of intent (not Critical Files)

Do not nag humans about stale DRAFT after they ordered execute — hygiene-fix or ignore when eng-log/git show execution.

## Size (soft)

| Target | Guidance |
|--------|----------|
| **~2–8 KB** | Typical single-feature plan |
| **8–12 KB** | Multi-track locks with short “why large” |
| **>12 KB** | Prefer **split** plans |

## Structure

Context · Recommended Approach · Critical Files · Existing utilities · **Phased Execution** (what + files + observable success) · Verification / **Acceptance**.

**Compliance:** one line citing `standard-plan-compliance-block.md` — **do not paste**.

## Do

- Product locks + Acceptance the coder can completeness-check.
- High-signal only; cite paths not long code dumps.
- Long forensics → `sandbox/research/<slug>.md` + link (or chat).
- **Finish greps in planning.** Never leave `unless` / `grep X` / “if that path walks…” in a plan about to be approved. Write the answer (`Kotlin: no edits.` or the exact `fun`).
- **Closed Critical Files table:** path + function + predicate. Last line: **Files not listed: no edits. Report extras in chat; do not implement.**
- **Named functions**, not “energy path.” Copy the skip/guard predicate once.
- **Fewest phases.** One file → usually one phase (skip + encode-catch in the same `.cpp` is one observable). Split when files or revert-units differ. Docs last. Do not add phases so you hit ~3–8.
- **Self-contained.** Executor re-reads this file + role pack only. Do not require memory of the last CODE LANDED plan. One sentence of prior-cycle context is enough.

## Do not

- Mandate Acknowledgment section
- Paste STANDARD BLOCK / AGENT_MANDATES
- Ultra-micro / max-granularity language (except post–inning recovery). Exact function names in Critical Files are **not** ultra-micro.
- Research assignments in the execute contract (`grep expandDiagnoseMany`, “unless a native-null path lacks it”)
- Optional extra sites (“and oriented energy if it walks a seed that small”)
- Multi-plan LANDED audit tables inside the next feature plan
- Device forensics novels in the execution contract
- Per-phase gate ritual repeated every phase (STANDARD BLOCK covers once)
- `./build_app @file` syntax in plan text
- Success lines that require discovering paths (“every native-null fallback”)

## Good target style

Short exact plans (e.g. one-weight UI tweak) or lock tables with clear Acceptance — not process essays.

## TODO-linked plans and research

- **Open** (`- [ ]`) TODO items that name a plan/research path: keep that file under `plans/` or `research/` — do **not** archive while the item is open.
- **Closed** (`- [x]`) items, or plans that are **obsolete** (done and superseded): archive the plan if nothing **open** still points at it; close/eliminate the TODO bullet if the work is obsolete (do not leave open TODOs aimed at dead plans).
- Prefer: close obsolete TODO first, then archive the plan.
