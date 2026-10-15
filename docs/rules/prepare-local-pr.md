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
   - **a. Plan.** Run `./prepare-pr-rewrite --plan-input [TARGET]` (`TARGET` defaults to `master`). Read the file it names: the net diff by file and hunk, the original commits behind each hunk, files whose changes cancel out, and debug/TODO leftovers. Do not re-derive these by hand; decide only the grouping. Write `$SANDBOX/PRs/rewrite-<branch>.md`: ordered steps, each with purpose, the hunks or parts of files, and why it builds alone. Order: prep and refactors, infrastructure, each feature with its tests, docs last. Only the net diff is used, so anything absent from the tip disappears.
   - **b. Findings and deviations.** List bugs, dead code, leftover debug, and inconsistencies under **Deviations** in the same file: finding, fix, step. Print the numbered step list (`N steps`, one line each) and the deviations, then wait for David's OK of the deviations (`STATUS: NEEDS-INPUT`). Fold only approved fixes into their steps. A new finding while building: print it and wait again.
   - **c. Start or redo.** `./prepare-pr-rewrite [TARGET]` tags the tip `backup-<branch>`, verifies the tag and resets this branch to the merge-base with `TARGET`. If the tag already exists (an earlier attempt), use `./prepare-pr-rewrite --redo [TARGET]`: same reset onto the current merge-base, the tag stays as the reference. Both refuse on uncommitted tracked changes and on untracked files that would collide; other untracked files stay. The log keeps its full content, infra is set to its expected version, everything else is the base, and the helper appends the `history rewrite started|redo` and `reset to <base> done` log entries. These are the only allowed resets; never run `git reset` yourself. Any ERROR: stop and report it. Never delete or move the tag.
   - **d. First commit: log + infra.** Commit exactly the paths the helper prints after `FIRST COMMIT` with the project-facts.md Commit path (message `Start history rewrite of <branch>: engineering log, infrastructure`). For a `RESOLVE` path first remove the conflict markers: keep `TARGET`'s version plus the branch's own change. No product change goes in this commit; no infra change goes in a later one.
   - **e. Build each step.** What remains is `git diff HEAD backup-<branch>`. Per step write the step's intermediate file states into the worktree (`git apply` of a partial patch, or the file's intermediate text when a hunk must be split). Never take a whole file when only part belongs to the step. `project-facts.md` and `TODO.md` change in the step that makes the fact true or adds or resolves the item. Then `./append-to-engineering-log` one entry for the step, and commit with the project-facts.md Commit path, naming the step's paths and `ENGINEERING_LOG.md`: imperative subject, body says why, no mention of false starts. A red build means revise the plan (move or merge steps); no broken commit, and the `AGENTS.md` Retry counting and `./reset-to-builds.sh` do not apply here. After each commit print `Step n/N done: <subject> (build OK, <sha>)`; on a plan revision print the revised list with the new N.
   - **f. Final check.** `./prepare-pr-rewrite --check [TARGET]`: the log at HEAD starts with the `backup-<branch>` copy and is fully committed, and every infra path equals its expected version (no conflict markers); on ERROR stop. It lists the product files that differ from `backup-<branch>` and the paths the original branch never changed. Run the project's tests (`project-facts.md` Testing) on the new tip. Review `git diff backup-<branch> HEAD` hunk by hunk: apart from the log, infra and `REVIEW` lines, it must consist only of the approved deviations. Any other difference: stop and report it. Print `Check n/3 done: <log+infra check|tests|deviation review> (<result>)` after each. Never go silent for long stretches.
3. Run **`./generate_pr.sh`** with plan paths → `$SANDBOX/PRs/PR-<branch>.md`. Include the review summary, the approved **Deviations** list, and any TODO items that merge should **close**.
4. **Third_party (VE consumers only):** if this branch touched `third_party/`, note in the PR: mode (ro/rw), lock sha vs HEAD, artifacts/patches, whether a library PR is also required.
5. Plans stay in `$SANDBOX/plans/`; the planner archives them with `/validate-plans`.
6. `./append-to-engineering-log` note: PR prepared, path to PR doc.
7. **Report** to David and stop: the plan, commits with build results, the test result, the deviations, the final diff, the original (`backup-<branch>`), and "ready for Master (launcher named in your skill) review + merge". Do not merge yourself.

## History rewrite rules

Goal: a logical progression from base to tip, as if written with perfect foresight. Each commit is small, has one reason, and builds. False starts, dead ends, debugging and scratch tests vanish. A file may change in several commits.

Wrong: one squash; one commit per file; soft reset then committing groups of whole files ("squash and segment").

**Engineering log** (`merge=ve-englog` in `.gitattributes`, `ENGINEERING_LOG.md`) is a record of what happened: append-only (`chattr +a`), never shortened, never reverted, never a source of deviations. The rewrite keeps its full content, the first commit carries it with the helper's two entries, and each later commit adds one entry via `./append-to-engineering-log`.
**Infrastructure** (paths in `rules.manifest` and the project root's `rules.manifest.local`, `third_party/*/libpin.toml`, `third_party/*/SOURCE.md`, `third_party/fetch-deps`) belongs to the orchestrator, not to product steps. The helper sets it to `TARGET`'s version when the branch only took a rules sync, else to the branch's copy or the 3-way merge, so the PR never reverts infra on `TARGET`; branch-only infra changes go in the first commit.
`project-facts.md` and `TODO.md` are ordinary files: they change with the commit that makes them true.
`CHANGELOG.md` is not a record: if it exists it may be rewritten to describe only the final kept changes in plain language, never mistakes.

## Forbidden

- `git reset` or `git tag` by hand, deleting or moving `backup-<branch>`, `git commit --amend` of published history.
- `gh pr create`, Graphite `gt submit`, `/pr-babysit`.
- Any push, and deploy (human only).
