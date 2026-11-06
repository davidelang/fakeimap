#!/bin/bash
# merge-branch-into-master.sh — Master-facing merge with special-file protocol.
# Usage: ./orch/merge-branch-into-master.sh <branch-name>
#
# Special files:
#   ENGINEERING_LOG.md — chattr +a: never worktree-replace; ve-englog appends tail.
#     After the move the blob is work-history/ENGINEERING_LOG.md and the root
#     name is a symlink. Index updates use the blob path, never mode 100644
#     on the symlink.
#   TODO.md / project-facts.md — merge=ve-special-ours; restore_special enforces master.
#   app/versionCode.txt — Play versionCode (deploy --play). Larger integer of the
#     two branches. The fast-forward path and a one-sided edit do not run the
#     merge driver, so versioncode_take_max enforces it after every successful path.
#
# Leaves merge UNCOMMITTED. Master finishes special files, then ./build_app --standard --test.
#
# Paths (in order):
#   1) Fast-forward: merge-base(HEAD,branch)==HEAD → index-first from branch tip (no git merge)
#   2) git merge --no-ff --no-commit (skip-worktree eng-log; no englog index=worktree poison)
#   3) Fallback: merge-tree + read-tree (index-first +a-safe)
#
# Failures always return non-zero. Do not trust a quiet exit without checking staged paths.

set -euo pipefail

USAGE_LINE="usage: ./orch/merge-branch-into-master.sh [-h] BRANCH"
usage() {
  cat <<EOF
$USAGE_LINE

Merge BRANCH into master, index-first, and leave the merge uncommitted.

options:
  -h, --help  show this help message and exit

Details: the comment at the top of ./orch/merge-branch-into-master.sh.
EOF
}
case "${1:-}" in
  -h|--help) usage; exit 0 ;;
  -[A-Za-z-]*) echo "$USAGE_LINE" >&2; echo "merge-branch-into-master.sh: error: unrecognized option: $1" >&2; exit 2 ;;
esac

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

if [ -x ./orch/install-merge-drivers.sh ]; then
  ./orch/install-merge-drivers.sh >/dev/null
else
  echo "WARNING: install-merge-drivers.sh missing; merge drivers may not run" >&2
fi

echo "=== merge-branch-into-master: merging $BRANCH into $CURRENT ==="
echo "Branch tip: $(git rev-parse --short "$BRANCH")  Base: $(git merge-base HEAD "$BRANCH" | cut -c1-8)"

while git stash list 2>/dev/null | head -1 | grep -q autostash; do
  echo "  Dropping stale autostash entry"
  git stash drop >/dev/null 2>&1 || break
done

pre_head=$(git rev-parse HEAD)

# Blob path. work-history/<name> once moved (root name is a symlink). Prefer the
# worktree file so a staged move is honored before that commit exists.
content_path() {
  local name="$1"
  if [ -f "work-history/$name" ] && [ ! -L "work-history/$name" ]; then
    printf 'work-history/%s\n' "$name"
  elif git cat-file -e "HEAD:work-history/$name" >/dev/null 2>&1; then
    printf 'work-history/%s\n' "$name"
  else
    printf '%s\n' "$name"
  fi
}
ENG_REL=$(content_path ENGINEERING_LOG.md)
TODO_REL=$(content_path TODO.md)
pre_todo=$(git rev-parse "HEAD:${TODO_REL}" 2>/dev/null || git rev-parse ":${TODO_REL}" 2>/dev/null || echo "")
pre_facts=$(git rev-parse HEAD:project-facts.md 2>/dev/null || echo "")

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
  if [ ! -f "$ENG_REL" ]; then
    return 0
  fi
  git update-index --no-skip-worktree -- "$ENG_REL" 2>/dev/null || true
  local hash
  hash=$(git rev-parse "HEAD:${ENG_REL}" 2>/dev/null || true)
  if [ -z "$hash" ]; then
    hash=$(git rev-parse ":${ENG_REL}" 2>/dev/null || true)
  fi
  if [ -z "$hash" ]; then
    return 0
  fi
  # 100644 is the blob. Never apply that mode to the root symlink path.
  git update-index --cacheinfo 100644,"$hash","$ENG_REL"
  echo "  $ENG_REL: index reset to HEAD (${hash:0:8})"
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
  # The three versions go next to the log (work-history/, else the top level), as
  # git-merge-drivers/ve-englog does; never /tmp. Removed below on every path.
  local tdir=.
  [ ! -d work-history ] || tdir=work-history
  ancestor=$(mktemp "$tdir/.englog-ancestor.XXXXXX") || { echo "ERROR: cannot create a temp file in $tdir" >&2; return 1; }
  ours=$(mktemp "$tdir/.englog-ours.XXXXXX") || { rm -f "$ancestor"; echo "ERROR: cannot create a temp file in $tdir" >&2; return 1; }
  theirs=$(mktemp "$tdir/.englog-theirs.XXXXXX") || { rm -f "$ancestor" "$ours"; echo "ERROR: cannot create a temp file in $tdir" >&2; return 1; }
  show_log() {
    local rev="$1" dest="$2"
    if git cat-file -e "$rev:work-history/ENGINEERING_LOG.md" >/dev/null 2>&1; then
      git show "$rev:work-history/ENGINEERING_LOG.md" > "$dest"
    elif git cat-file -e "$rev:ENGINEERING_LOG.md" >/dev/null 2>&1; then
      git show "$rev:ENGINEERING_LOG.md" > "$dest"
    else
      : > "$dest"
    fi
  }
  show_log "$base" "$ancestor"
  show_log "$pre_head" "$ours"
  show_log "$branch" "$theirs"
  local drc=0
  ./git-merge-drivers/ve-englog "$ancestor" "$ours" "$theirs" "" ENGINEERING_LOG.md || drc=$?
  rm -f "$ancestor" "$ours" "$theirs"
  [ "$drc" -eq 0 ] || { echo "ERROR: ve-englog failed (exit $drc)" >&2; return "$drc"; }
  echo "  ENGINEERING_LOG.md: ve-englog append complete"
}

checkout_merged_worktree_skip_special() {
  local f
  while IFS= read -r f; do
    [ -z "$f" ] && continue
    case "$f" in
      ENGINEERING_LOG.md|TODO.md|project-facts.md|work-history/ENGINEERING_LOG.md|work-history/TODO.md) continue ;;
    esac
    # Never overwrite the merge helper mid-recovery
    [ "$f" = "merge-branch-into-master.sh" ] && continue
    git checkout-index -f -- "$f" 2>/dev/null || true
  done < <(git diff --cached --name-only --diff-filter=ACMR)
}

write_merge_head() {
  printf '%s\n' "$(git rev-parse "$BRANCH")" > "$(git rev-parse --git-path MERGE_HEAD)"
}

# app/versionCode.txt is the integer deploy --play ships. Keep the larger of the
# two branch tips. One-sided edits and the fast-forward path never call the
# merge driver, so this runs after every successful merge path. No such file on
# either tip: nothing to do (this host has no Play version).
versioncode_take_max() {
  local branch="$1"
  local have_o=0 have_t=0
  if git cat-file -e "${pre_head}:app/versionCode.txt" >/dev/null 2>&1; then
    have_o=1
  fi
  if git cat-file -e "${branch}:app/versionCode.txt" >/dev/null 2>&1; then
    have_t=1
  fi
  if [ "$have_o" -eq 0 ] && [ "$have_t" -eq 0 ]; then
    return 0
  fi
  if [ "$have_o" -eq 1 ] && [ "$have_t" -eq 0 ]; then
    git checkout "$pre_head" -- app/versionCode.txt || return 1
    echo "  app/versionCode.txt: only master has it; kept"
    return 0
  fi
  if [ "$have_o" -eq 0 ] && [ "$have_t" -eq 1 ]; then
    git checkout "$branch" -- app/versionCode.txt || return 1
    echo "  app/versionCode.txt: only $branch has it; kept"
    return 0
  fi
  if [ ! -x ./git-merge-drivers/ve-versioncode ]; then
    echo "ERROR: ve-versioncode missing; cannot choose the Play versionCode" >&2
    return 1
  fi
  # Temps sit next to the file (app/ when that directory exists). Never /tmp.
  local tdir=.
  [ -d app ] && tdir=app
  local ancestor ours theirs
  ancestor=$(mktemp "$tdir/.versioncode-ancestor.XXXXXX") || {
    echo "ERROR: cannot create a temp file in $tdir" >&2
    return 1
  }
  ours=$(mktemp "$tdir/.versioncode-ours.XXXXXX") || {
    rm -f "$ancestor"
    echo "ERROR: cannot create a temp file in $tdir" >&2
    return 1
  }
  theirs=$(mktemp "$tdir/.versioncode-theirs.XXXXXX") || {
    rm -f "$ancestor" "$ours"
    echo "ERROR: cannot create a temp file in $tdir" >&2
    return 1
  }
  if ! git show "${pre_head}:app/versionCode.txt" > "$ours"; then
    rm -f "$ancestor" "$ours" "$theirs"
    return 1
  fi
  if ! git show "${branch}:app/versionCode.txt" > "$theirs"; then
    rm -f "$ancestor" "$ours" "$theirs"
    return 1
  fi
  : > "$ancestor"
  local drc=0
  ./git-merge-drivers/ve-versioncode "$ancestor" "$ours" "$theirs" "" app/versionCode.txt || drc=$?
  if [ "$drc" -ne 0 ]; then
    rm -f "$ancestor" "$ours" "$theirs"
    echo "ERROR: ve-versioncode failed (exit $drc)" >&2
    return "$drc"
  fi
  mkdir -p app || { rm -f "$ancestor" "$ours" "$theirs"; return 1; }
  if ! cp "$ours" app/versionCode.txt; then
    rm -f "$ancestor" "$ours" "$theirs"
    return 1
  fi
  rm -f "$ancestor" "$ours" "$theirs"
  git add -- app/versionCode.txt || return 1
  echo "  app/versionCode.txt: staged $(tr -d '[:space:]' < app/versionCode.txt) (larger of the two branches)"
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
      ENGINEERING_LOG.md|TODO.md|project-facts.md|work-history/ENGINEERING_LOG.md|work-history/TODO.md) continue ;;
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
      ENGINEERING_LOG.md|TODO.md|project-facts.md|work-history/ENGINEERING_LOG.md|work-history/TODO.md) continue ;;
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
  restore_special "$TODO_REL" "$pre_todo"
  restore_special project-facts.md "$pre_facts"
  verify_index_blob "$TODO_REL" "$pre_todo"
  verify_index_blob project-facts.md "$pre_facts"
  checkout_merged_worktree_skip_special
  write_merge_head
  englog_merge_via_driver "$branch" || true
  # Stage eng-log only via hash (worktree may be +a; git add can fail).
  # The path is the blob, not the root symlink.
  if [ -f "$ENG_REL" ]; then
    local h
    h=$(git hash-object "$ENG_REL")
    git update-index --cacheinfo 100644,"$h","$ENG_REL" 2>/dev/null || \
      git add -- "$ENG_REL" 2>/dev/null || true
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
  git update-index --skip-worktree -- "$ENG_REL" 2>/dev/null || true
  if ! git merge --no-ff --no-commit --no-autostash "$branch"; then
    git update-index --no-skip-worktree -- "$ENG_REL" 2>/dev/null || true
    return 1
  fi
  restore_special "$TODO_REL" "$pre_todo"
  restore_special project-facts.md "$pre_facts"
  verify_index_blob "$TODO_REL" "$pre_todo"
  verify_index_blob project-facts.md "$pre_facts"
  englog_merge_via_driver "$branch" || true
  if [ -f "$ENG_REL" ]; then
    local h
    h=$(git hash-object "$ENG_REL")
    git update-index --cacheinfo 100644,"$h","$ENG_REL" 2>/dev/null || \
      git add -- "$ENG_REL" 2>/dev/null || true
  fi
  git update-index --no-skip-worktree -- "$ENG_REL" 2>/dev/null || true
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
    git update-index --no-skip-worktree -- "$ENG_REL" 2>/dev/null || true
    merge_index_first "$BRANCH"
    merge_rc=$?
  fi
fi

if [ "$merge_rc" -eq 0 ]; then
  versioncode_take_max "$BRANCH" || merge_rc=$?
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

app/versionCode.txt (when either branch has it)
  The staged file is the larger integer of the two branches. deploy --play ships that number.
  A non-integer fails the merge.

POST-MERGE CHECK (do this before ./build_app):
  git diff --cached --name-only
  → must include feature paths (e.g. .kt), not ONLY ENGINEERING_LOG.md
  If only eng-log is staged → FAILED. Do not build_app. See docs/obsolete/MERGE_POSTMORTEM_IMPROVE_PUMP_CLASSIFICATION.md

Next: complete TODO + project-facts protocol → git status → ./build_app --standard --test (never --play)
========================================================================
EOF

if [ "$merge_rc" -ne 0 ]; then
  echo "merge-branch-into-master: FAILED (rc=$merge_rc)" >&2
  exit "$merge_rc"
fi
echo "merge-branch-into-master: staged (no commit). Complete special files, then ./build_app --standard --test." >&2
exit 0
