---
name: master-merge
description: >
  Review and merge a feature branch into master: local PR review, plan
  adherence, special-file merge (ENGINEERING_LOG, TODO, project-facts), post-merge
  gate, and build gate. Same process on every host. Use for review PR, merge branch,
  or /master-merge.
when-to-use: "review PR, merge branch, master merge, integrate branch, /master-merge"
disable-model-invocation: true
---

# master-merge

`$SANDBOX` is `project.config` `sandbox_dir` (else `sandbox_path`).

## Review
1. Read `$SANDBOX/PRs/PR-<branch>.md`. It holds the plans and the backup tag.
2. Check that `git log master..<branch>` is clean and has compiling commits. Compare `git diff master..<branch>` against the plans.
3. Reject anything the plan did not list: silent improvements, refactors, or "best practice" changes. Ask the branch's coder to revert them.

## Merge
1. Run `python3 $SANDBOX/audit_merge.py <branch>` if it exists.
2. Run `./install-merge-drivers.sh`, then `./merge-branch-into-master.sh <branch>` from the master worktree.
3. `ENGINEERING_LOG.md`: the ve-englog driver appends the branch tail via `./append-to-engineering-log`. Never take the branch file. No `chattr -a`.
4. `TODO.md` and `project-facts.md`: master's version stays (ve-special-ours). Then apply the branch delta with `./todo-close` / `./todo-append` and by editing facts by hand.
   **Before changing any Testing section (project-wide or `## Worktree:`) of `project-facts.md`, ask David and wait.**
5. Post-merge gate: `git diff --cached --name-only` must list the feature paths, not just `ENGINEERING_LOG.md`. Otherwise the merge FAILED; do not build.
6. Build gate: `./build_app`, or `project.config` `build_command`. If there is none, say "no compile gate".
7. Never set or move `works`. Tell David he can run `./remove_worktree.sh <branch>`. Fixes to merge scripts land on the orchestration checkout first and are then published.

End with the STATUS line.
