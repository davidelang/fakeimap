# prepare-local-pr: shared procedure (Coder)

Used by the Grok and Cursor `prepare-local-pr` skills; each skill adds only its agent-specific bits.

**Owner:** the branch's coder session (it did the work and saw the findings). Replaying the branch's own net diff plus David-approved deviations is the one exception to the coder's "never edit code".

You prepare a **local** PR document for Master review. This project does **not** use GitHub PR create/babysit workflows. Same steps on VehicleExpenses and library hosts.

`$SANDBOX` is `project.config` `sandbox_dir` (relative to the worktree, or absolute). If it is unset or not a directory, stop and report it.

## Preconditions

- Feature branch (not `master` or `orchestration`).
- Implementation for the approved plan is done, and the project-facts.md Build succeeded on the tip.
- Confirm `pwd` once; use `./helper` only (no `cd … &&`).

## Steps

1. **Pre-submit code review**: diff vs the approved plan path(s). Fail on scope creep or silent improvements. List residual risks.
2. **History rewrite**:
   - **a. Plan** from the net diff `git diff $(git merge-base $TARGET HEAD) HEAD` (`$TARGET` defaults to `master`) plus `git log $TARGET..HEAD` for intent. Write `$SANDBOX/PRs/rewrite-<branch>.md`: ordered steps, each with purpose, the hunks or parts of files, and why it builds alone. Order: prep and refactors, infrastructure, each feature with its tests, docs last. Only the net diff is used, so anything absent from the tip disappears.
   - **b. Findings and deviations.** List bugs, dead code, leftover debug, and inconsistencies under **Deviations** in the same file: finding, fix, step. Print the numbered step list (`N steps`, one line each) and the deviations, then wait for David's OK of the deviations (`STATUS: NEEDS-INPUT`). Fold only approved fixes into their steps. A new finding while building: print it and wait again.
   - **c. Start.** `./prepare-pr-rewrite [TARGET]`. It refuses on uncommitted changes, tags the tip `backup-<branch>`, verifies the tag, then resets this branch to the merge-base, record files excepted. This is the only allowed reset; the tag keeps the history. Any ERROR: stop and report it. Never delete or move the tag.
   - **d. Record commit.** The Commit path refuses other uncommitted tracked files, so commit the record files first, verbatim, with the project-facts.md Commit path (message `Add engineering log for <branch>`).
   - **e. Build each step.** What remains is `git diff HEAD backup-<branch>`. Per step write the step's intermediate file states into the worktree (`git apply` of a partial patch, or the file's intermediate text when a hunk must be split). Never take a whole file when only part belongs to the step. Commit with the project-facts.md Commit path, naming the paths: imperative subject, body says why, no mention of false starts. A red build means revise the plan (move or merge steps); no broken commit, and the `AGENTS.md` Retry counting and `./reset-to-builds.sh` do not apply here. After each commit print `Step n/N done: <subject> (build OK, <sha>)`; on a plan revision print the revised list with the new N.
   - **f. Final check.** `./prepare-pr-rewrite --check`: record files must equal `backup-<branch>`; on ERROR stop. Run the project's tests (`project-facts.md` Testing) on the new tip. Review `git diff backup-<branch> HEAD` hunk by hunk: it must consist only of the approved deviations. Any other difference: stop and report it. Print `Check n/3 done: <record check|tests|deviation review> (<result>)` after each. Never go silent for long stretches.
3. Run **`./generate_pr.sh`** with plan paths → `$SANDBOX/PRs/PR-<branch>.md`. Include the review summary, the approved **Deviations** list, and any TODO items that merge should **close**.
4. **Third_party (VE consumers only):** if this branch touched `third_party/`, note in the PR: mode (ro/rw), lock sha vs HEAD, artifacts/patches, whether a library PR is also required.
5. Plans stay in `$SANDBOX/plans/`; the planner archives them with `/validate-plans`.
6. `./append-to-engineering-log` note: PR prepared, path to PR doc.
7. **Report** to David and stop: the plan, commits with build results, the test result, the deviations, the final diff, the original (`backup-<branch>`), and "ready for Master (launcher named in your skill) review + merge". Do not merge yourself.

## History rewrite rules

Goal: a logical progression from base to tip, as if written with perfect foresight. Each commit is small, has one reason, and builds. False starts, dead ends, debugging and scratch tests vanish. A file may change in several commits.

Wrong: one squash; one commit per file; soft reset then committing groups of whole files ("squash and segment").

**Record files** keep the real history: the paths with `merge=ve-englog` or `merge=ve-special-ours` in `.gitattributes` (`ENGINEERING_LOG.md`, `TODO.md`, `project-facts.md`); the helper prints the list. Never edit them, and do not run `./append-to-engineering-log`, `./todo-append`, or `./todo-close` during steps 2a-2f. The helper leaves their tip content in the worktree; step 2d commits it verbatim. They are never a source of deviations.
`CHANGELOG.md` is not a record file: if it exists it may be rewritten to describe only the final kept changes in plain language, never mistakes.

## Forbidden

- `git reset` or `git tag` by hand, deleting or moving `backup-<branch>`, `git commit --amend` of published history.
- `gh pr create`, Graphite `gt submit`, `/pr-babysit`.
- Any push, and deploy (human only).
