#!/usr/bin/env bash
# fix-multiuser-git-hosts.sh
#
# Repair multi-user git DAC so ai-planner (and all ai-shared members) can use
# git log / status / commit on host repos under GIT_HOME.
#
# DOCUMENTED MODEL (docs/reference/MULTIUSER_GIT_VE_PARITY.md) — not "whatever
# VE currently has" (VE can drift):
#   .git and every directory under it:  mode 2770, group ai-shared, setgid
#   owner typically dlang
#   core.sharedRepository=group
#   HEAD/config/packed-refs/refs/logs: group-readable/writable (ug+rw)
#
# WHY THIS MATTERS
#   ai-planner is in ai-shared but NOT in ai-code.
#   If objects/refs/HEAD/config are group ai-code and mode 660/2770 without
#   other access, planner gets Permission denied on git log (Unix, not Landlock).
#
# Usage (run as dlang; uses sudo for chgrp/chmod of mixed owners):
#   ./fix-multiuser-git-hosts.sh -n|--dry-run
#   ./fix-multiuser-git-hosts.sh
#   ./fix-multiuser-git-hosts.sh -a|--audit-only
#   -h|--help
#   HOSTS="VehicleExpenses-automated orchestration-example" ./fix-multiuser-git-hosts.sh
#
# Default HOSTS:
#   VehicleExpenses-automated remotetable extractmail orchestration-example
#
set -uo pipefail

for a in "$@"; do
  case "$a" in
    -h|--help) sed -n '2,35p' "$0"; exit 0 ;;
  esac
done

if [[ "$(id -un)" != "dlang" && "$(id -un)" != "root" && "${ALLOW_NON_DLANG:-0}" != "1" ]]; then
  echo "ERROR: run as dlang (or root with GIT_HOME set). whoami=$(id -un)" >&2
  exit 1
fi

DRY=0
AUDIT_ONLY=0
parsed="$(getopt -o hna -l help,dry-run,audit-only -- "$@")" || exit 2
eval set -- "$parsed"
while true; do
  case "$1" in
    -h|--help) sed -n '2,35p' "$0"; exit 0 ;;
    -n|--dry-run) DRY=1; shift ;;
    -a|--audit-only) AUDIT_ONLY=1; shift ;;
    --) shift; break ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done
if [[ $# -gt 0 ]]; then
  echo "unknown arg: $1" >&2
  exit 2
fi

if [[ -n "${SUDO_USER:-}" && "$SUDO_USER" != "root" ]]; then
  _sh=$(getent passwd "$SUDO_USER" | cut -d: -f6)
  GIT_HOME="${GIT_HOME:-$_sh/git}"
fi
GIT_HOME="${GIT_HOME:-/home/dlang/git}"
HOSTS="${HOSTS:-VehicleExpenses-automated remotetable extractmail orchestration-example}"

GIT_GROUP=ai-shared
DIR_MODE=2770
PRIMARY_USER="${PRIMARY_USER:-dlang}"

echo "================================================================"
echo "fix-multiuser-git-hosts"
echo "  GIT_HOME=$GIT_HOME  whoami=$(id -un) DRY=$DRY AUDIT_ONLY=$AUDIT_ONLY"
echo "  group=$GIT_GROUP  dir_mode=$DIR_MODE  sharedRepository=group"
echo "  ai-shared members: $(getent group ai-shared 2>/dev/null || echo '?')"
echo "  ai-planner groups: $(id ai-planner 2>/dev/null || echo '?')"
echo "================================================================"

# Approximate whether ai-planner can read a path (DAC only, no ACL).
planner_can_read() {
  local path="$1"
  python3 - "$path" <<'PY'
import os, stat, sys, pwd, grp
path = sys.argv[1]
try:
    st = os.stat(path)
except OSError as e:
    print(f"NO ({e})")
    sys.exit(0)
planner = pwd.getpwnam("ai-planner")
gids = {planner.pw_gid}
for g in ("ai-shared", "ai-sandbox"):
    try:
        gids.add(grp.getgrnam(g).gr_gid)
    except KeyError:
        pass
mode, uid, gid = st.st_mode, st.st_uid, st.st_gid
if uid == planner.pw_uid:
    ok = bool(mode & stat.S_IRUSR)
elif gid in gids:
    ok = bool(mode & stat.S_IRGRP)
else:
    ok = bool(mode & stat.S_IROTH)
print("YES" if ok else "NO")
PY
}

audit_repo() {
  local root="$1"
  local g="$root/.git"
  echo ""
  echo "######## AUDIT $root ########"
  if [[ ! -d "$root" ]]; then
    echo "  MISSING root"
    return 1
  fi
  if [[ -f "$g" ]]; then
    echo "  .git is a worktree file → resolve common dir"
    local common
    common=$(git -c "safe.directory=$root" -c "safe.directory=*" -C "$root" rev-parse --git-common-dir 2>/dev/null || true)
    if [[ -n "$common" && "$common" != /* ]]; then
      common="$root/$common"
    fi
    if [[ -z "$common" || ! -d "$common" ]]; then
      echo "  cannot resolve common dir"
      return 1
    fi
    g="$common"
    echo "  common=$g"
  fi
  if [[ ! -d "$g" ]]; then
    echo "  MISSING $g"
    return 1
  fi

  local badg bads shared
  badg=$(find "$g" ! -group "$GIT_GROUP" 2>/dev/null | wc -l)
  bads=$(find "$g" -type d ! -perm -2000 2>/dev/null | wc -l)
  shared=$(git --git-dir="$g" config --get core.sharedRepository 2>/dev/null || echo UNSET)

  stat -c '  .git     %a %U:%G %A' "$g"
  [[ -d "$g/objects" ]] && stat -c '  objects  %a %U:%G %A' "$g/objects"
  [[ -d "$g/refs" ]] && stat -c '  refs     %a %U:%G %A' "$g/refs"
  for f in HEAD config packed-refs; do
    if [[ -e "$g/$f" ]]; then
      printf '  %-12s ' "$f"
      stat -c '%a %U:%G' "$g/$f"
    fi
  done

  echo "  sharedRepository=$shared"
  echo "  paths not group $GIT_GROUP: $badg"
  echo "  dirs without setgid bit: $bads"
  echo "  sample non-$GIT_GROUP (max 8):"
  find "$g" ! -group "$GIT_GROUP" 2>/dev/null | head -8 | sed 's/^/    /'
  echo -n "  ai-planner can read HEAD? "
  planner_can_read "$g/HEAD"
  echo -n "  ai-planner can read config? "
  planner_can_read "$g/config"
  echo -n "  ai-planner can read objects/? "
  planner_can_read "$g/objects"

  if [[ "$badg" -ne 0 || "$bads" -ne 0 || "$shared" != "group" ]]; then
    echo "  RESULT: NEEDS_FIX"
    return 1
  fi
  # planner must be able to read HEAD
  local pr
  pr=$(planner_can_read "$g/HEAD")
  if [[ "$pr" != "YES" ]]; then
    echo "  RESULT: NEEDS_FIX (planner cannot read HEAD)"
    return 1
  fi
  echo "  RESULT: OK"
  return 0
}

fix_repo() {
  local root="$1"
  local g="$root/.git"
  echo ""
  echo "######################################################################"
  echo "# FIX $root"
  echo "######################################################################"

  if [[ ! -d "$root" ]]; then
    echo "MISSING $root"
    return 1
  fi

  if [[ -f "$g" ]]; then
    local common
    common=$(git -c "safe.directory=$root" -c "safe.directory=*" -C "$root" rev-parse --git-common-dir 2>/dev/null || true)
    [[ -n "$common" && "$common" != /* ]] && common="$root/$common"
    if [[ -z "$common" || ! -d "$common" ]]; then
      echo "  cannot resolve common git dir for worktree"
      return 1
    fi
    g="$common"
    echo "  linked worktree → fixing common dir $g"
  fi

  if [[ ! -d "$g" ]]; then
    echo "MISSING $g"
    return 1
  fi

  echo "BEFORE:"
  audit_repo "$root" || true

  if [[ "$DRY" -eq 1 || "$AUDIT_ONLY" -eq 1 ]]; then
    echo "  (no changes: DRY=$DRY AUDIT_ONLY=$AUDIT_ONLY)"
    return 0
  fi

  echo "  git config core.sharedRepository group..."
  git --git-dir="$g" config core.sharedRepository group 2>/dev/null \
    || sudo -u "$PRIMARY_USER" git --git-dir="$g" config core.sharedRepository group 2>/dev/null \
    || true

  echo "  sudo chgrp -R $GIT_GROUP $g ..."
  if ! sudo chgrp -R "$GIT_GROUP" "$g"; then
    echo "  ERROR: chgrp failed" >&2
    return 1
  fi

  # Owner: prefer dlang on tree (optional; mixed owners ok if group is right)
  if id "$PRIMARY_USER" &>/dev/null; then
    echo "  sudo chown -R $PRIMARY_USER:$GIT_GROUP $g (best-effort)..."
    sudo chown -R "$PRIMARY_USER:$GIT_GROUP" "$g" 2>/dev/null || \
      sudo chgrp -R "$GIT_GROUP" "$g" || true
  fi

  echo "  sudo find -type d -exec chmod $DIR_MODE ..."
  if ! sudo find "$g" -type d -exec chmod "$DIR_MODE" {} +; then
    echo "  ERROR: chmod dirs failed" >&2
    return 1
  fi

  # Group-readable/writable metadata (not world); leave loose object blobs as-is mostly
  echo "  ensure ug+rw on refs/logs/info/HEAD/config..."
  sudo find "$g/refs" "$g/logs" "$g/info" "$g/hooks" "$g/worktrees" \
    -type f -exec chmod ug+rw {} + 2>/dev/null || true
  for f in config HEAD packed-refs description; do
    [[ -e "$g/$f" ]] && sudo chmod ug+rw "$g/$f" 2>/dev/null || true
  done
  # Loose objects: if 600/660 without group read after chgrp, ug+r
  sudo find "$g/objects" -type f ! -perm -040 -exec chmod ug+r {} + 2>/dev/null || true

  git --git-dir="$g" config core.sharedRepository group 2>/dev/null \
    || sudo -u "$PRIMARY_USER" git --git-dir="$g" config core.sharedRepository group 2>/dev/null \
    || true

  echo "AFTER:"
  if audit_repo "$root"; then
    echo "  RESULT: FIXED_OK"
    return 0
  fi
  echo "  RESULT: STILL_BAD"
  return 1
}

rc=0
for h in $HOSTS; do
  root="$GIT_HOME/$h"
  if [[ "$AUDIT_ONLY" -eq 1 ]]; then
    audit_repo "$root" || rc=1
  else
    fix_repo "$root" || rc=1
  fi
done

echo ""
echo "================================================================"
if [[ "$AUDIT_ONLY" -eq 1 ]]; then
  echo "Audit done (rc=$rc). Fix with: $0   (as dlang, no --audit-only)"
elif [[ "$DRY" -eq 1 ]]; then
  echo "Dry-run done. Apply with: $0"
else
  echo "Done rc=$rc. Verify as planner:"
  echo "  sudo -u ai-planner git -C $GIT_HOME/VehicleExpenses-automated log -1 --oneline"
fi
echo "================================================================"
exit "$rc"
