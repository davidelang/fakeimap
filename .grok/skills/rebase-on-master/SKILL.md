---
name: rebase-on-master
description: >
  Rebase a feature branch onto master (or another integration tip) while
  keeping the branch's ENGINEERING_LOG.md, TODO.md, and project-facts.md.
  Do not text-merge those specials; master merge handles them later. Use when
  the user says rebase, rebase on master, catch up with master, or
  /rebase-on-master.
when-to-use: "rebase, rebase on master, catch up master, replay commits, /rebase-on-master"
---

# rebase-on-master

Keep **this branch's** copies of **only** these three files:

- `ENGINEERING_LOG.md`
- `TODO.md`
- `project-facts.md`

Do **not** take master's copies of those three. They are reconciled only at
**master merge** (`master-merge` / `MASTER_AGENT_MANDATE.md`).

**Every other path** uses a normal rebase. On any non-special conflict:
**stop, show both sides, ask the human.** Do not pick ours/theirs.

## Why not merge drivers / `-X ours` on the specials?

`.gitattributes` `merge=ve-special-ours` / `ve-englog` are for **merge into
master**. During `git rebase`, Git's "ours" is the **onto** commit (master).
Drivers would keep **master's** specials — the opposite of this skill.

Do **not** pass `git rebase -X ours` or `-X theirs` for the whole rebase
just to handle specials. That would change conflict resolution for **all**
files. Restore only the three paths from `$FEAT` (below).

## Preconditions

- Feature branch (not `master` / `orchestration` unless the user explicitly
  rebases that).
- Clean tracked tree (`git status --porcelain -uno` empty) or the user has
  said what to do with dirt. Do not `stash --staged` as a workaround.
- `pwd` once. No `cd … && ./helper`.
- Reset only via `AGENT_MANDATES.md` §6 if you must abort.

## Steps

1. Record the pre-rebase tip (feature specials live here):

   ```bash
   git rev-parse --abbrev-ref HEAD
   git rev-parse HEAD
   ```

   Store as `FEAT` (the SHA). Do not invent a relative ref.

2. Rebase onto the tip the user named (usually `master` in this worktree's
   remote-tracking sense). From the feature worktree:

   ```bash
   git rebase master
   ```

   If the user named another onto (e.g. `origin/master`), use that exact name.

3. **Conflicts on specials:** do not edit hunks. Restore the feature file:

   ```bash
   git checkout "$FEAT" -- ENGINEERING_LOG.md TODO.md project-facts.md
   git add ENGINEERING_LOG.md TODO.md project-facts.md
   ```

   Then continue the rebase (`git rebase --continue`) after other conflicts
   are resolved (or after the human has decided those other conflicts).

4. **Conflicts on other files:** stop. Print the path, both sides (rebase
   ours = onto/master, theirs = the commit being replayed), and **ask the
   human**. Do not pick a side. Do not force `$FEAT` onto those paths.

5. **After the rebase succeeds** (drivers may have silently taken master):

   ```bash
   git checkout "$FEAT" -- ENGINEERING_LOG.md TODO.md project-facts.md
   ```

   If the tree is clean vs `$FEAT` for those paths, this is a no-op.

6. If checkout of `ENGINEERING_LOG.md` fails because of `chattr +a`: **stop
   and report**. Do not `chattr -a`. The feature file already on disk may
   already be correct; do not replace it with master's.

7. Do not commit specials "to match master". A commit is needed only if
   step 5 dirtied the tree; message: keep branch specials after rebase.

8. `./build_app` if this is an app worktree and the rebase changed product
   files.

## Forbidden

- Text-merging specials (no combined TODO, no spliced facts).
- Global `git rebase -X ours` / `-X theirs` to “handle specials” (would
  rewrite every file’s conflict policy).
- `git checkout --ours` on the three specials during rebase (that is
  **master**). Other files: do not auto-pick a side; ask the human.
- `chattr -a` on the eng-log.
- Relative resets (`HEAD~`, `HEAD^`) to undo a rebase. Abort with
  `git rebase --abort` while in progress, or §6 if already finished and the
  user approves a allowed reset.

## Afterward

Tell the user the new tip SHA. Specials still wait for master merge.
