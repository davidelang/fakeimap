# prepare-local-pr: shared procedure (Coder)

Used by the Grok and Cursor `prepare-local-pr` skills; each skill adds only its agent-specific bits.

You prepare a **local** PR document for Master review. This project does **not** use GitHub PR create/babysit workflows.

**Applies to VehicleExpenses and library hosts** (remotetable, extractmail, …) with the same steps.

## Sandbox path

Resolve once at start:

1. `project.config` → `sandbox_dir` (or legacy `sandbox_path`)
2. Else if `./dev-ai-interaction/` exists → that
3. Else `./sandbox/`

Call this **`$SANDBOX`**. Plans live under `$SANDBOX/plans/`. PRs under `$SANDBOX/PRs/`. Historical plans under `$SANDBOX/historical-plans/`.

## Preconditions

- Feature branch (not `master` or `orchestration`).
- Implementation for the approved plan is done.
- Build gate when the host has one: VE → `./build_app` succeeded; library → `./build` or documented tests if present; if no build yet, note that in the PR.
- Confirm `pwd` once; use `./helper` only (no `cd … &&`).

## Steps

1. **Pre-submit code review** (you): diff vs the approved plan path(s). Fail if scope creep / silent improvements. List residual risks.
2. **History rewrite** (below).
3. Run **`./generate_pr.sh`** with plan paths → `$SANDBOX/PRs/PR-<branch>.md`. Include review summary and any TODO items that merge should **close**.
4. **Third_party (VE consumers only):** if this branch touched `third_party/`, note in PR: mode (ro/rw), lock sha vs HEAD, artifacts/patches, whether a library PR is also required.
5. **Archive completed plans:** move designated CODE LANDED / done plan file(s) from `$SANDBOX/plans/` to `$SANDBOX/historical-plans/`.
6. After the final check, `./append-to-engineering-log` note: PR prepared, path to PR doc.
7. Stop and tell the user: ready for Master (launcher named in your skill) independent review + merge. Do not merge yourself.

## History rewrite

Goal: a logical progression from base to tip, as if written with perfect foresight. Each commit is small, has one reason, and builds. False starts, dead ends, debugging and scratch tests vanish. A file may change in several commits.

Wrong: one squash; one commit per file; soft reset then committing groups of whole files ("squash and segment").

**Record files** keep the real history: the paths with `merge=ve-englog` or `merge=ve-special-ours` in `.gitattributes` (`ENGINEERING_LOG.md`, `TODO.md`, `project-facts.md`); the helper prints the list. Never edit them, and do not run `./append-to-engineering-log`, `./todo-append`, or `./todo-close` during the rewrite. The helper leaves their tip content in the worktree. Commit them verbatim in one last commit, `Add engineering log for <branch>` (or with the first step whose build needs one). They are never a source of deviations.
`CHANGELOG.md` is not a record file: if it exists it may be rewritten to describe only the final kept changes in plain language, never mistakes.

1. **Plan** from the net diff `git diff $(git merge-base master HEAD) HEAD` plus `git log master..HEAD` for intent. Write `$SANDBOX/PRs/rewrite-<branch>.md`: ordered steps, each with purpose, the hunks or parts of files, and why it builds alone. Order: prep and refactors, infrastructure, each feature with its tests, docs last. Only the net diff is used, so anything absent from the tip disappears. Then print the numbered step list (`N steps`, one line each) before starting.
2. **Findings and deviations.** While planning and building, list bugs, dead code, leftover debug, and inconsistencies. Fold each fix into the step where it belongs (as if done right the first time), and record it in the same file under **Deviations**: finding, fix, step.
3. **Start.** `./prepare-pr-rewrite [TARGET]` (default master). It refuses on uncommitted changes, tags the tip `backup-<branch>`, verifies the tag, then resets this branch to the merge-base, record files excepted. This is the only allowed reset; the tag keeps the history. Any ERROR: stop and report it. Never delete or move the tag.
4. **Build each step.** What remains is `git diff HEAD backup-<branch>`. Per step write the step's intermediate file states into the worktree (`git apply` of a partial patch, or the file's intermediate text when a hunk must be split). Never take a whole file when only part belongs to the step. Commit with `./build_app @msg <paths>`: imperative subject, body says why, no mention of false starts. A red build means revise the plan (move or merge steps); no broken commit. After each commit print `Step n/N done: <subject> (build OK, <sha>)`; on a plan revision print the revised list with the new N.
5. **Final check.** `./prepare-pr-rewrite --check`: record files must equal `backup-<branch>`; on ERROR stop. Run the project's tests (`project-facts.md` Testing) on the new tip. Review `git diff backup-<branch> HEAD` hunk by hunk: it must consist only of the listed deviations. Any unlisted difference: stop and report it. Print `Check n/3 done: <record check|tests|deviation review> (<result>)` after each. Never go silent for long stretches.
6. **Report** to David: the plan, commits with their build results, the test result, the deviations list, and that final diff. The original is `backup-<branch>`.

## Forbidden

- `git reset` or `git tag` by hand, deleting or moving `backup-<branch>`, `git commit --amend` of published history.
- `gh pr create`, Graphite `gt submit`, `/pr-babysit`, force-push without project rules.
- Deploy / `git push` (human only).
