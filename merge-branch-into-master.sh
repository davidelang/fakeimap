#!/bin/bash
# merge-branch-into-master.sh — Master-facing merge with special-file protocol.
# Usage: ./merge-branch-into-master.sh <branch-name>
#
# Special files:
#   ENGINEERING_LOG.md — chattr +a: never worktree-replace; ve-englog appends tail.
#   TODO.md / project-facts.md — merge=ve-special-ours; restore_special enforces master.
#
# Leaves merge UNCOMMITTED. Master finishes special files, then ./build_app.
#
# Paths (in order):
#   1) Fast-forward: merge-base(HEAD,branch)==HEAD → index-first from branch tip (no git merge)
#   2) git merge --no-ff --no-commit (skip-worktree eng-log; no englog index=worktree poison)
#   3) Fallback: merge-tree + read-tree (index-first +a-safe)
#
# Failures always return non-zero. Do not trust a quiet exit without checking staged paths.

set -euo pipefail

BRANCH="${1:-}"
if [ -z "$BRANCH" ]; then
  echo "Usage: $0 <branch-name>" >&2
  exit 1
fi

ROOT=$(git rev-parse --show-toplevel)
cd "$ROOT"

CURRENT=$(git rev-parse --abbrev-ref HEAD)
if [ "$CURRENT" != "master" ]; then
  echo "WARNING: current branch is '$CURRENT' (expected master). Continuing anyway." >&2
fi

if ! git rev-parse --verify "$BRANCH" >/dev/null 2>&1; then
  echo "ERROR: unknown branch/ref: $BRANCH" >&2
  exit 1
fi

if [ -x ./install-merge-drivers.sh ]; then
  ./install-merge-drivers.sh >/dev/null
else
  echo "WARNING: install-merge-drivers.sh missing; merge drivers may not run" >&2
fi

echo "=== merge-branch-into-master: merging $BRANCH into $CURRENT ==="
echo "Branch tip: $(git rev-parse --short "$BRANCH")  Base: $(git merge-base HEAD "$BRANCH" | cut -c1-8)"

while git stash list 2>/dev/null | head -1 | grep -q autostash; do
  echo "  Dropping stale autostash entry"
  git stash drop >/dev/null 2>&1 || break
done

pre_todo=$(git rev-parse HEAD:TODO.md 2>/dev/null || echo "")
pre_facts=$(git rev-parse HEAD:project-facts.md 2>/dev/null || echo "")
pre_head=$(git rev-parse HEAD)

# Paths that change on the branch vs pre_head (for post-merge guard)
branch_delta_paths() {
  git diff --name-only "$pre_head"..."$BRANCH" 2>/dev/null || true
}

restore_special() {
  local f="$1" blob="$2"
  if [ -z "$blob" ]; then
    return 0
  fi
  echo "  restore_special: master base for $f"
  git show "$blob" > "$f"
  git add -f "$f" 2>/dev/null || git add "$f"
}

verify_index_blob() {
  local f="$1" expected="$2"
  local actual
  actual=$(git rev-parse ":$f" 2>/dev/null || echo "")
  if [ "$actual" != "$expected" ]; then
    echo "ERROR: staged $f blob ${actual:0:8} != master ${expected:0:8}" >&2
    return 1
  fi
  echo "  verify: $f index matches master (${expected:0:8})"
}

# Index eng-log = HEAD blob (not worktree). Avoids "local changes would be overwritten"
# when worktree already has a partial ve-englog append from a failed attempt.
englog_reset_index_to_head() {
  if [ ! -f ENGINEERING_LOG.md ]; then
    return 0
  fi
  git update-index --no-skip-worktree ENGINEERING_LOG.md 2>/dev/null || true
  local hash
  hash=$(git rev-parse HEAD:ENGINEERING_LOG.md 2>/dev/null || true)
  if [ -z "$hash" ]; then
    return 0
  fi
  git update-index --cacheinfo 100644,"$hash",ENGINEERING_LOG.md
  echo "  ENGINEERING_LOG.md: index reset to HEAD (${hash:0:8})"
}

# Clear partial/failed merge staging so read-tree can run.
clear_partial_index() {
  echo "  Clearing partial index state (git reset HEAD) before index-first path"
  git reset HEAD >/dev/null 2>&1 || true
  git merge --abort 2>/dev/null || true
  englog_reset_index_to_head
}

englog_merge_via_driver() {
  local branch="$1"
  local base ancestor ours theirs
  if [ ! -x ./git-merge-drivers/ve-englog ]; then
    echo "WARNING: ve-englog missing; eng-log append skipped" >&2
    return 1
  fi
  base=$(git merge-base "$pre_head" "$branch")
  ancestor=$(mktemp)
  ours=$(mktemp)
  theirs=$(mktemp)
  git show "$base":ENGINEERING_LOG.md > "$ancestor" 2>/dev/null || : > "$ancestor"
  git show "$pre_head":ENGINEERING_LOG.md > "$ours" 2>/dev/null || : > "$ours"
  git show "$branch":ENGINEERING_LOG.md > "$theirs"
  ./git-merge-drivers/ve-englog "$ancestor" "$ours" "$theirs" "" ENGINEERING_LOG.md
  rm -f "$ancestor" "$ours" "$theirs"
  echo "  ENGINEERING_LOG.md: ve-englog append complete"
}

checkout_merged_worktree_skip_special() {
  local f
  while IFS= read -r f; do
    [ -z "$f" ] && continue
    [ "$f" = "ENGINEERING_LOG.md" ] && continue
    [ "$f" = "TODO.md" ] && continue
    [ "$f" = "project-facts.md" ] && continue
    # Never overwrite the merge helper mid-recovery
    [ "$f" = "merge-branch-into-master.sh" ] && continue
    git checkout-index -f -- "$f" 2>/dev/null || true
  done < <(git diff --cached --name-only --diff-filter=ACMR)
}

write_merge_head() {
  printf '%s\n' "$(git rev-parse "$BRANCH")" > "$(git rev-parse --git-path MERGE_HEAD)"
}

# After staging: ensure branch non-special deltas are present (not eng-log-only success).
assert_staged_feature_files() {
  local branch="$1"
  local expected=0
  local staged_non_special=0
  local p

  while IFS= read -r p; do
    [ -z "$p" ] && continue
    case "$p" in
      ENGINEERING_LOG.md|TODO.md|project-facts.md) continue ;;
      *) expected=$((expected + 1)) ;;
    esac
  done < <(branch_delta_paths)

  if [ "$expected" -eq 0 ]; then
    echo "  note: branch has no non-special path delta vs pre-merge HEAD (specials-only or empty)"
    return 0
  fi

  while IFS= read -r p; do
    [ -z "$p" ] && continue
    case "$p" in
      ENGINEERING_LOG.md|TODO.md|project-facts.md) continue ;;
      *) staged_non_special=$((staged_non_special + 1)) ;;
    esac
  done < <(git diff --cached --name-only 2>/dev/null || true)

  if [ "$staged_non_special" -eq 0 ]; then
    echo "ERROR: merge-branch-into-master: FAILED — staged only special files (or nothing)," >&2
    echo "  but branch $branch changes $expected non-special path(s)." >&2
    echo "  Do NOT run ./build_app. Inspect: git diff --cached --name-only" >&2
    echo "  Staged now:" >&2
    git diff --cached --name-only >&2 || true
    return 1
  fi
  echo "  verify: staged $staged_non_special non-special path(s) (branch had $expected non-special delta paths)"
  return 0
}

finish_specials_and_englog() {
  local branch="$1"
  restore_special TODO.md "$pre_todo"
  restore_special project-facts.md "$pre_facts"
  verify_index_blob TODO.md "$pre_todo"
  verify_index_blob project-facts.md "$pre_facts"
  checkout_merged_worktree_skip_special
  write_merge_head
  englog_merge_via_driver "$branch" || true
  # Stage eng-log only via hash (worktree may be +a; git add can fail)
  if [ -f ENGINEERING_LOG.md ]; then
    local h
    h=$(git hash-object ENGINEERING_LOG.md)
    git update-index --cacheinfo 100644,"$h",ENGINEERING_LOG.md 2>/dev/null || \
      git add ENGINEERING_LOG.md 2>/dev/null || true
  fi
}

# Fast-forward: branch is strictly ahead of HEAD. No git merge (avoids +a eng-log worktree replace).
merge_ff_index() {
  local branch="$1"
  local tree
  echo "  path: fast-forward index (merge-base == HEAD; skip git merge)"
  clear_partial_index
  tree=$(git rev-parse "$branch^{tree}")
  if [ -z "$tree" ]; then
    echo "ERROR: cannot resolve tree for $branch" >&2
    return 1
  fi
  if ! git read-tree --reset "$tree"; then
    echo "ERROR: read-tree failed on FF path (often eng-log index skew); try clear + retry" >&2
    return 1
  fi
  finish_specials_and_englog "$branch"
}

merge_index_first() {
  local branch="$1"
  local base tree mt_out mt_rc
  echo "  path: index-first (merge-tree + read-tree)"
  clear_partial_index
  base=$(git merge-base HEAD "$branch")
  mt_out=$(git merge-tree --write-tree --merge-base="$base" HEAD "$branch" 2>&1)
  mt_rc=$?
  if [ "$mt_rc" -ne 0 ]; then
    echo "ERROR: merge-tree failed (rc=$mt_rc); not read-tree'ing" >&2
    printf '%s\n' "$mt_out" >&2
    return 1
  fi
  tree=$(printf '%s\n' "$mt_out" | head -n 1)
  if [ -z "$tree" ]; then
    echo "ERROR: merge-tree produced empty tree" >&2
    return 1
  fi
  echo "  merge-tree OK → ${tree:0:8}"
  if ! git read-tree --reset "$tree"; then
    echo "ERROR: read-tree failed (often +a ENGINEERING_LOG.md / dirty index)" >&2
    return 1
  fi
  finish_specials_and_englog "$branch"
}

# Happy path: do NOT sync eng-log index to worktree (poisons merge when WT ahead of HEAD).
merge_git_ort() {
  local branch="$1"
  echo "  path: git merge --no-ff --no-commit"
  englog_reset_index_to_head
  git update-index --skip-worktree ENGINEERING_LOG.md 2>/dev/null || true
  if ! git merge --no-ff --no-commit --no-autostash "$branch"; then
    git update-index --no-skip-worktree ENGINEERING_LOG.md 2>/dev/null || true
    return 1
  fi
  restore_special TODO.md "$pre_todo"
  restore_special project-facts.md "$pre_facts"
  verify_index_blob TODO.md "$pre_todo"
  verify_index_blob project-facts.md "$pre_facts"
  englog_merge_via_driver "$branch" || true
  if [ -f ENGINEERING_LOG.md ]; then
    local h
    h=$(git hash-object ENGINEERING_LOG.md)
    git update-index --cacheinfo 100644,"$h",ENGINEERING_LOG.md 2>/dev/null || \
      git add ENGINEERING_LOG.md 2>/dev/null || true
  fi
  git update-index --no-skip-worktree ENGINEERING_LOG.md 2>/dev/null || true
}

merge_rc=0
set +e
base_now=$(git merge-base HEAD "$BRANCH")
if [ "$base_now" = "$(git rev-parse HEAD)" ]; then
  # Branch is fast-forward from HEAD (or equal)
  if [ "$(git rev-parse HEAD)" = "$(git rev-parse "$BRANCH")" ]; then
    echo "  already up-to-date with $BRANCH"
    merge_rc=0
  else
    merge_ff_index "$BRANCH"
    merge_rc=$?
  fi
else
  merge_git_ort "$BRANCH"
  merge_rc=$?
  if [ "$merge_rc" -ne 0 ]; then
    echo "  git merge failed (rc=$merge_rc); falling back to index-first +a-safe path" >&2
    git merge --abort 2>/dev/null || true
    git update-index --no-skip-worktree ENGINEERING_LOG.md 2>/dev/null || true
    merge_index_first "$BRANCH"
    merge_rc=$?
  fi
fi

if [ "$merge_rc" -eq 0 ]; then
  assert_staged_feature_files "$BRANCH"
  merge_rc=$?
fi
set -e

cat <<EOF

========================================================================
SPECIAL FILES — REQUIRED FOR EVERY MERGE (not only when these paths diff)
Branch: $BRANCH
Merge exit code: $merge_rc
========================================================================

ENGINEERING_LOG.md
  ve-englog append-only (never chattr -a / unlink). Third version = master + branch tail.

TODO.md / project-facts.md
  merge=ve-special-ours keeps master in git index; restore_special enforces master on disk.
  Still required: todo-close / todo-append vs branch+PR; project-facts prune/add vs branch delta.

POST-MERGE CHECK (do this before ./build_app):
  git diff --cached --name-only
  → must include feature paths (e.g. .kt), not ONLY ENGINEERING_LOG.md
  If only eng-log is staged → FAILED. Do not build_app. See docs/obsolete/MERGE_POSTMORTEM_IMPROVE_PUMP_CLASSIFICATION.md

Next: complete TODO + project-facts protocol → git status → ./build_app
========================================================================
EOF

if [ "$merge_rc" -ne 0 ]; then
  echo "merge-branch-into-master: FAILED (rc=$merge_rc)" >&2
  exit "$merge_rc"
fi
echo "merge-branch-into-master: staged (no commit). Complete special files, then build_app." >&2
exit 0
