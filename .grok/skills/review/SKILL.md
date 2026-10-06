---
name: review
description: >
  Local review of uncommitted changes, a named local branch vs master (or host
  default), or $SANDBOX/PRs/PR-<branch>.md. Writes a review file under
  $SANDBOX/reviews/. Never GitHub/GitLab/Graphite. Use when asked to review,
  review my changes, review this PR (meaning the local PR markdown), or /review.
when-to-use: "review, code review, review my changes, review this PR, /review"
---

# review (local)

This **project** skill replaces the bundled GitHub `review`. Review **this repo
on disk** only.

**Applies to VehicleExpenses and library hosts** (same sandbox resolution).

## Sandbox

1. `project.config` → `sandbox_dir` / `sandbox_path`
2. Else `./dev-ai-interaction/` or `./sandbox/`

Call it **`$SANDBOX`**. Create `$SANDBOX/reviews/` if needed.

## Targets (pick one)

- **Uncommitted:** `git status --porcelain` and `git diff` / `git diff --cached`.
- **Named local branch:** `git log master..<branch>` and `git diff master..<branch>`
  (library host: default integration branch if not `master`).
- **Local PR doc:** `$SANDBOX/PRs/PR-<branch>.md` if it exists. Read it; still
  diff the branch. A GitHub PR number or URL is **not** a target — say so and
  use the local branch or local PR markdown instead.

## Plan compare

If the user named an approved plan path, diff against that contract. If they
did not, diff vs `master` (or host default) and say no plan was named.

## Output

Write:

`$SANDBOX/reviews/review-<branch-or-wt>-YYYYMMDD-HHMM.md`

Include: target, plan path or “none named”, findings, residual risks. Short
summary in chat. Do not post remotes.

## Roles

- **Planner:** sandbox write only (the review file). No app source edits.
- **Coder:** same; no extra source edits from this skill.
- **Master:** optional before `master-merge`. Does **not** replace `master-merge`.

`prepare-local-pr` remains the path to **create** `$SANDBOX/PRs/PR-<branch>.md`.

## Forbidden

`gh`, `glab`, Graphite (`gt`), GitHub/GitLab APIs, push, merge, `./deploy`.
