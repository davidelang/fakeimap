#!/usr/bin/env bash
# deploy-postcheckout-git-safe.sh
#
# Publish checkout/git-safe permission scripts from this VE orchestration SoT
# to first-party / example hosts (and optionally every worktree under them).
#
# What this fixes (must stay true after copy):
#   - hooks/post-checkout must NOT run fix-perms --all
#   - hooks/post-checkout must NOT chown/chmod the common .git store
#   - fix-perms must not chown -R :ai-code into common .git
#   - fix-perms must not chmod 660 a .git *directory*
#
# Default targets (under GIT_HOME):
#   remotetable  extractmail  orchestration-example
#
# Usage (run as dlang — needs write on host trees):
#   ./deploy-postcheckout-git-safe.sh
#   ./deploy-postcheckout-git-safe.sh -c|--commit
#   ./deploy-postcheckout-git-safe.sh -n|--dry-run
#   ./deploy-postcheckout-git-safe.sh -a|--also-ve   # copy into VE worktrees too
#   -p|--push  -h|--help
#   HOSTS="remotetable extractmail" ./deploy-postcheckout-git-safe.sh --commit
#
# Env:
#   GIT_HOME   absolute host-clone root; else project.config git_home=
#   HOSTS      space-separated host dir names under GIT_HOME
#
# Does NOT copy project.config. Does NOT push unless --push.
# Does NOT run fix-multiuser-git-hosts (separate host DAC repair).
set -uo pipefail

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

for a in "$@"; do
  case "$a" in
    -h|--help) sed -n '2,35p' "$0"; exit 0 ;;
  esac
done

if [[ -z "${GIT_HOME:-}" && -f "$SRC/project.config" ]]; then
  GIT_HOME=$(sed -n 's/^git_home=//p' "$SRC/project.config" | tr -d '\r' | head -1)
fi
if [[ -z "${GIT_HOME:-}" || "$GIT_HOME" == @@* ]]; then
  echo "ERROR: set GIT_HOME to an absolute path, or put git_home=/absolute/... in $SRC/project.config" >&2
  exit 1
fi
if [[ "$GIT_HOME" != /* ]]; then
  echo "ERROR: GIT_HOME must be absolute (got: $GIT_HOME)" >&2
  exit 1
fi

HOSTS="${HOSTS:-remotetable extractmail orchestration-example}"

DRY=0
DO_COMMIT=0
DO_PUSH=0
DO_VE=0

parsed="$(getopt -o hncap -l help,dry-run,commit,push,also-ve -- "$@")" || exit 2
eval set -- "$parsed"
while true; do
  case "$1" in
    -h|--help) sed -n '2,35p' "$0"; exit 0 ;;
    -n|--dry-run) DRY=1; shift ;;
    -c|--commit) DO_COMMIT=1; shift ;;
    -p|--push) DO_PUSH=1; DO_COMMIT=1; shift ;;
    -a|--also-ve) DO_VE=1; shift ;;
    --) shift; break ;;
    *) echo "unknown arg: $1 (try --help)" >&2; exit 2 ;;
  esac
done
if [[ $# -gt 0 ]]; then
  echo "unknown arg: $1 (try --help)" >&2
  exit 2
fi

# Tracked paths to publish (relative to repo root)
PUBLISH_FILES=(
  hooks/post-checkout
  fix-perms
  deploy-postcheckout-git-safe.sh
)

die() { echo "ERROR: $*" >&2; exit 1; }
info() { echo ""; echo "==> $*"; }

# --- preflight SoT ---
[[ -f "$SRC/hooks/post-checkout" ]] || die "missing SoT hooks/post-checkout"
[[ -f "$SRC/fix-perms" ]] || die "missing SoT fix-perms"
# Reject real invocations only (comments may still mention the ban).
if grep -nE '^[[:space:]]*(\./)?fix-perms|^[[:space:]]*sudo .*\./fix-perms|^[[:space:]]*\./fix-perms' \
  "$SRC/hooks/post-checkout" >/dev/null 2>&1; then
  die "SoT hooks/post-checkout still invokes fix-perms (refusing to publish)"
fi
if grep -nE '^[[:space:]]*(\./)?set-(worktree|sandbox)-perms' \
  "$SRC/hooks/post-checkout" >/dev/null 2>&1; then
  die "SoT hooks/post-checkout still invokes legacy whole-tree set-*-perms"
fi
# Must mention the ban / scoped behavior
grep -q 'COMMON_GITDIR\|git-common-dir' "$SRC/hooks/post-checkout" \
  || die "SoT hooks/post-checkout missing common-git guard"
grep -q 'chown_worktree_code_group\|repair_common_gitdir' "$SRC/fix-perms" \
  || die "SoT fix-perms missing git-safe helpers"
if grep -nE 'chown -R :\$CODE_GROUP "\$wt"|chown -R "\$TARGET_OWNER:\$CODE_GROUP" "\$wt"' \
  "$SRC/fix-perms" >/dev/null 2>&1; then
  die "SoT fix-perms still blanket chown -R CODE_GROUP on whole worktree"
fi

echo "Source of truth: $SRC"
echo "GIT_HOME=$GIT_HOME"
echo "HOSTS=$HOSTS"
echo "DRY=$DRY COMMIT=$DO_COMMIT PUSH=$DO_PUSH ALSO_VE=$DO_VE"
echo "whoami=$(id -un)"

copy_file() {
  local rel="$1" dest_root="$2"
  local from="$SRC/$rel" to="$dest_root/$rel"
  [[ -e "$from" ]] || { echo "  SKIP missing source $rel"; return 0; }
  local from_r to_r
  from_r="$(readlink -f "$from" 2>/dev/null || realpath "$from" 2>/dev/null || echo "$from")"
  to_r="$(readlink -f "$to" 2>/dev/null || realpath "$to" 2>/dev/null || echo "$to")"
  if [[ "$from_r" == "$to_r" ]]; then
    return 0
  fi
  if [[ "$DRY" -eq 1 ]]; then
    echo "  DRY: cp $rel → $dest_root/"
    return 0
  fi
  mkdir -p "$(dirname "$to")" || die "mkdir failed for $to (need write as $(id -un)?)"
  cp -a --no-preserve=timestamps "$from" "$to" 2>/dev/null || cp -a "$from" "$to" \
    || die "cp failed $from → $to"
  case "$rel" in
    fix-perms|hooks/post-checkout|*.sh)
      chmod 775 "$to" 2>/dev/null || chmod +x "$to" 2>/dev/null || true
      ;;
  esac
  echo "  ok $rel"
}

# Install live common hook so next checkout uses the safe script immediately.
install_live_hook() {
  local dest_root="$1"
  local template="$dest_root/hooks/post-checkout"
  local common live
  if [[ "$DRY" -eq 1 ]]; then
    echo "  DRY: install live .git/hooks/post-checkout from template"
    return 0
  fi
  [[ -f "$template" ]] || { echo "  SKIP live hook (no template)"; return 0; }
  common=$(git -c "safe.directory=$dest_root" -c "safe.directory=*" -C "$dest_root" rev-parse --git-common-dir 2>/dev/null || true)
  case "$common" in
    /*) ;;
    "") common="$dest_root/.git" ;;
    *) common="$dest_root/$common" ;;
  esac
  live="$common/hooks/post-checkout"
  mkdir -p "$(dirname "$live")" 2>/dev/null || true
  # Prefer content-only copy (not -a): template is often dlang:ai-code; live must be
  # ai-shared or fix-multiuser-git-hosts --audit-only reports NEEDS_FIX for one path.
  if cp --no-preserve=mode,ownership,timestamps "$template" "$live" 2>/dev/null \
     || cp "$template" "$live" 2>/dev/null; then
    chmod 775 "$live" 2>/dev/null || true
    # Multi-user git model: everything under common .git is group ai-shared
    chown :ai-shared "$live" 2>/dev/null \
      || chown "$(id -un):ai-shared" "$live" 2>/dev/null \
      || echo "  WARN: could not chown :ai-shared $live (run as dlang or fix-multiuser)"
    echo "  live hook → $live ($(stat -c '%a %U:%G' "$live" 2>/dev/null || echo '?'))"
  else
    echo "  WARN: could not install live hook at $live"
  fi
}

git_c() {
  local dest="$1"
  shift
  git -c "safe.directory=$dest" -c "safe.directory=*" -C "$dest" "$@"
}

git_safe() {
  local dest="$1"
  git_c "$dest" rev-parse --is-inside-work-tree >/dev/null 2>&1
}

list_worktrees() {
  local root="$1"
  if git_safe "$root"; then
    git_c "$root" worktree list --porcelain 2>/dev/null | awk '/^worktree /{print $2}'
    return 0
  fi
  echo "$root"
}

commit_target() {
  local dest="$1" label="$2"
  if ! git_safe "$dest"; then
    echo "  (not a git worktree — skip commit) $dest"
    return 0
  fi
  (
    cd "$dest" || exit 0
    git -c "safe.directory=$dest" -c "safe.directory=*" add -f \
      hooks/post-checkout \
      fix-perms \
      deploy-postcheckout-git-safe.sh \
      2>/dev/null || true
    if git -c "safe.directory=$dest" -c "safe.directory=*" diff --cached --quiet 2>/dev/null; then
      echo "  (no staged changes) $label"
      return 0
    fi
    git -c "safe.directory=$dest" -c "safe.directory=*" commit -m "$(cat <<'EOF'
## post-checkout scoped perms + fix-perms git-safe (from VE)

- post-checkout: no fix-perms --all; branch-only scoped chmod; never common .git DAC
- fix-perms: exclude common .git from ai-code chown; no chmod 660 on .git dir
EOF
)" || echo "  WARN: commit failed on $label"
    if [[ "$DO_PUSH" -eq 1 ]]; then
      git -c "safe.directory=$dest" -c "safe.directory=*" push || echo "  WARN: push failed on $label"
    fi
  )
}

publish_root() {
  local dest="$1" label="$2"
  info "Publish → $label ($dest)"
  if [[ ! -d "$dest" ]]; then
    echo "  MISSING $dest — skip"
    return 0
  fi
  if [[ ! -w "$dest" ]]; then
    echo "  ERROR: not writable by $(id -un): $dest"
    echo "  Run this script as dlang (or fix DAC), then re-run."
    return 1
  fi
  local rel
  for rel in "${PUBLISH_FILES[@]}"; do
    copy_file "$rel" "$dest"
  done
  install_live_hook "$dest"

  # Also push into other worktrees of this host (copies of fix-perms used by local hooks/scripts)
  local wt
  while IFS= read -r wt; do
    [[ -n "$wt" ]] || continue
    [[ "$wt" == "$dest" ]] && continue
    [[ -d "$wt" ]] || continue
    if [[ ! -w "$wt" ]]; then
      echo "  skip unwritable worktree $wt"
      continue
    fi
    echo "  worktree $wt"
    for rel in "${PUBLISH_FILES[@]}"; do
      copy_file "$rel" "$wt"
    done
    # Live hook is shared via common .git — already installed once
  done < <(list_worktrees "$dest")

  if [[ "$DO_COMMIT" -eq 1 ]]; then
    commit_target "$dest" "$label"
  fi
  return 0
}

# Sanity: refuse if SoT itself still has poison path (belt)
FAIL=0
info "Targets"
for h in $HOSTS; do
  publish_root "$GIT_HOME/$h" "$h" || FAIL=1
done

if [[ "$DO_VE" -eq 1 ]]; then
  # SoT already correct; still install live hook + other VE worktrees' file copies
  publish_root "$SRC" "VehicleExpenses-automated" || FAIL=1
fi

info "Done (fail=$FAIL)"
if [[ "$FAIL" -ne 0 ]]; then
  echo "One or more hosts failed (often: not dlang / not writable)."
  echo "Retry as dlang from VE root:"
  echo "  ./deploy-postcheckout-git-safe.sh --commit"
  exit 1
fi
echo "Optional: ./fix-multiuser-git-hosts.sh --audit-only   # confirm all hosts OK"
exit 0
