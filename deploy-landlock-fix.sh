#!/usr/bin/env bash
# deploy-landlock-fix.sh
#
# Publish agent-session Landlock stack (helper + launch wire + config + smoke
# tools) from this VE orchestration SoT to first-party / example hosts and
# every git worktree under them, then optionally commit only those paths.
#
# Fixes of note (must stay in agent-landlock):
#   - Always grant $HOME/.grok so Grok trust/session creation is not EACCES
#   - Wire via .grok/lib/grok-launch-common.sh after sudo -u, before grok
#
# Default targets (under GIT_HOME):
#   remotetable  extractmail  orchestration-example
#
# Usage:
#   ./deploy-landlock-fix.sh                 # copy only
#   ./deploy-landlock-fix.sh -c|--commit     # copy + git add/commit per worktree
#   ./deploy-landlock-fix.sh -c|--commit -p|--push
#   ./deploy-landlock-fix.sh -n|--dry-run
#   ./deploy-landlock-fix.sh -a|--also-ve    # also all VE worktrees (incl. SoT)
#   -h|--help
#   HOSTS="remotetable extractmail" ./deploy-landlock-fix.sh --commit
#
# Env / config (no hard-coded machine paths as success defaults):
#   GIT_HOME   absolute host-clone root; else project.config git_home= next to script
#   HOSTS      space-separated host dir names under GIT_HOME (default below)
#
# Does NOT copy project.config (local-only / gitignored).
# Does NOT push unless --push. Prefer running as a user who can write the trees.
set -uo pipefail

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

USAGE_LINE="usage: deploy-landlock-fix.sh [-h] [-n] [-c] [-p] [-a]"
usage() {
  cat <<EOF
$USAGE_LINE

Copy the agent-session Landlock files from this orchestration root to each
host under GIT_HOME and its worktrees; optionally commit them.

options:
  -h, --help       show this help message and exit
  -n, --dry-run    report what would be copied; write nothing
  -c, --commit     also commit the copied paths in each worktree
  -p, --push       also push (implies --commit)
  -a, --also-ve    also every worktree of this orchestration root

Files copied, host selection (HOSTS, GIT_HOME) and what is never copied: the
comment at the top of deploy-landlock-fix.sh.
EOF
}

for a in "$@"; do
  case "$a" in
    -h|--help) usage; exit 0 ;;
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

# Default hosts the user asked to publish to
HOSTS="${HOSTS:-remotetable extractmail orchestration-example}"

DRY=0
DO_COMMIT=0
DO_PUSH=0
DO_VE=0

parsed="$(getopt -n deploy-landlock-fix.sh -o hncap -l help,dry-run,commit,push,also-ve -- "$@")" || { echo "$USAGE_LINE" >&2; exit 2; }
eval set -- "$parsed"
while true; do
  case "$1" in
    -h|--help) usage; exit 0 ;;
    -n|--dry-run) DRY=1; shift ;;
    -c|--commit) DO_COMMIT=1; shift ;;
    -p|--push) DO_PUSH=1; DO_COMMIT=1; shift ;;
    -a|--also-ve) DO_VE=1; shift ;;
    --) shift; break ;;
    *) echo "$USAGE_LINE" >&2; echo "deploy-landlock-fix.sh: error: unrecognized option: $1" >&2; exit 2 ;;
  esac
done
if [[ $# -gt 0 ]]; then
  echo "$USAGE_LINE" >&2
  echo "deploy-landlock-fix.sh: error: unrecognized argument: $1" >&2
  exit 2
fi

# Paths relative to SoT / each worktree root
LANDLOCK_FILES=(
  agent-landlock
  landlock.config
  landlock.config.example
  landlock-write-probe
  landlock-smoke-matrix
  .grok/lib/grok-launch-common.sh
  fix-multiuser-git-hosts.sh
  fix-ve-git-shared.sh
  deploy-landlock-fix.sh
  .grok/hooks/stop-completeness-gate.sh
  .grok/hooks/stop-completeness-gate.json
)

die() { echo "ERROR: $*" >&2; exit 1; }
info() { echo ""; echo "==> $*"; }

# --- preflight SoT ---
[[ -f "$SRC/agent-landlock" ]] || die "missing SoT $SRC/agent-landlock"
[[ -f "$SRC/.grok/lib/grok-launch-common.sh" ]] || die "missing SoT grok-launch-common.sh"
grep -q 'agent-landlock' "$SRC/.grok/lib/grok-launch-common.sh" \
  || die "SoT grok-launch-common.sh does not wire agent-landlock"
grep -q 'HOME/.grok' "$SRC/agent-landlock" || grep -q '".grok"' "$SRC/agent-landlock" \
  || die "SoT agent-landlock missing \$HOME/.grok grant (session fix)"
# Sanity: helper reports ABI or unavailable without crashing
if ! "$SRC/agent-landlock" --status >/dev/null 2>&1; then
  # status exits 1 when unavailable — still a valid helper
  out="$("$SRC/agent-landlock" --status 2>&1 || true)"
  [[ "$out" == available* || "$out" == unavailable* ]] \
    || die "SoT agent-landlock --status unexpected: $out"
fi

echo "Source of truth: $SRC"
echo "GIT_HOME=$GIT_HOME"
echo "HOSTS=$HOSTS"
echo "DRY=$DRY COMMIT=$DO_COMMIT PUSH=$DO_PUSH ALSO_VE=$DO_VE"

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
  mkdir -p "$(dirname "$to")"
  # Avoid noisy "preserving times" failures on some cross-uid trees
  cp -a --no-preserve=timestamps "$from" "$to" 2>/dev/null || cp -a "$from" "$to"
  case "$rel" in
    agent-landlock|landlock-write-probe|landlock-smoke-matrix|*.sh)
      chmod 775 "$to" 2>/dev/null || chmod +x "$to" 2>/dev/null || true
      ;;
  esac
}

# Ensure .gitattributes has manage-configs lines for landlock.config (no full overwrite).
ensure_gitattributes_landlock() {
  local dest_root="$1"
  local ga="$dest_root/.gitattributes"
  if [[ "$DRY" -eq 1 ]]; then
    echo "  DRY: ensure landlock filter lines in .gitattributes"
    return 0
  fi
  if [[ ! -f "$ga" ]]; then
    {
      echo "# manage-configs (smudge/clean @@ tokens)"
      echo "landlock.config filter=manage-configs"
      echo "landlock.config.example filter=manage-configs"
    } >"$ga"
    echo "  created .gitattributes with landlock filter lines"
    return 0
  fi
  if ! grep -qE '^[[:space:]]*landlock\.config[[:space:]]+filter=manage-configs' "$ga" 2>/dev/null; then
    {
      echo ""
      echo "# landlock (session agent-landlock config; smudge from project.config)"
      echo "landlock.config filter=manage-configs"
      echo "landlock.config.example filter=manage-configs"
    } >>"$ga"
    echo "  appended landlock filter lines to .gitattributes"
  fi
}

ensure_gitignore_grok_lib() {
  local dest_root="$1"
  local gi="$dest_root/.gitignore"
  [[ -f "$gi" ]] || return 0
  if grep -qE '^lib/' "$gi" 2>/dev/null && ! grep -q '!.grok/lib/' "$gi" 2>/dev/null; then
    if [[ "$DRY" -eq 1 ]]; then
      echo "  DRY: add !.grok/lib/ exception to .gitignore"
      return 0
    fi
    printf '\n# Track Grok pack launcher library (not Android/native lib/)\n!.grok/lib/\n!.grok/lib/**\n' >>"$gi"
    echo "  note: added !.grok/lib/ exception to .gitignore"
  fi
}

# git with safe.directory override (orchestration-example etc. trip dubious ownership)
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
  # Prefer git worktree list (covers paths not under root/*/)
  if git_safe "$root"; then
    git_c "$root" worktree list --porcelain 2>/dev/null | awk '/^worktree /{print $2}'
    return 0
  fi
  # Fallback: root + immediate child worktrees
  echo "$root"
  local d base
  for d in "$root"/*/; do
    [[ -d "$d" ]] || continue
    base=$(basename "$d")
    case "$base" in
      sandbox|third_party|docs|artifact|apps-script|.git) continue ;;
    esac
    if [[ -f "${d}.git" || -d "${d}.git" ]]; then
      echo "${d%/}"
    fi
  done
}

commit_target() {
  local dest="$1" label="$2"
  if ! git_safe "$dest"; then
    echo "  (not a git worktree — skip commit) $dest"
    return 0
  fi
  (
    cd "$dest" || exit 0
    # Stage only landlock publish paths (+ attrs/gitignore if we touched them)
    git -c "safe.directory=$dest" -c "safe.directory=*" add -f \
      agent-landlock \
      landlock.config \
      landlock.config.example \
      landlock-write-probe \
      landlock-smoke-matrix \
      .grok/lib/grok-launch-common.sh \
      fix-multiuser-git-hosts.sh \
      fix-ve-git-shared.sh \
      deploy-landlock-fix.sh \
      .grok/hooks/stop-completeness-gate.sh \
      .grok/hooks/stop-completeness-gate.json \
      .gitattributes \
      .gitignore \
      2>/dev/null || true

    if git -c "safe.directory=$dest" -c "safe.directory=*" diff --cached --quiet 2>/dev/null; then
      echo "  (no commit — landlock paths already current)"
      return 0
    fi

    git -c "safe.directory=$dest" -c "safe.directory=*" commit -m "$(cat <<'EOF'
chore: landlock + multi-user git fix scripts + free-form launcher flags

Publish agent-landlock (.grok grants), smoke matrix, grok-launch-common
(GROK_SANDBOX/GROK_WORKTREE), fix-multiuser-git-hosts, Stop completeness
hook (opt-in VE_STOP_COMPLETENESS=1). From VE SoT.
EOF
)"
    echo "  committed $(git -c "safe.directory=$dest" -c "safe.directory=*" rev-parse --short HEAD) on $(git -c "safe.directory=$dest" -c "safe.directory=*" branch --show-current 2>/dev/null || echo '?') [$label]"

    if [[ "$DO_PUSH" -eq 1 ]]; then
      local push_rc=0
      if git -c "safe.directory=$dest" -c "safe.directory=*" rev-parse --abbrev-ref '@{u}' >/dev/null 2>&1; then
        git -c "safe.directory=$dest" -c "safe.directory=*" push || push_rc=$?
      else
        git -c "safe.directory=$dest" -c "safe.directory=*" push -u origin "$(git -c "safe.directory=$dest" -c "safe.directory=*" branch --show-current)" || push_rc=$?
      fi
      if [[ "$push_rc" -eq 0 ]]; then
        echo "  pushed"
      else
        echo "  WARN push failed (rc=$push_rc) — commit is local only [$label]" >&2
      fi
    fi
  )
}

deploy_to() {
  local dest="$1"
  local label="$2"
  info "Deploy → $label ($dest)"
  if [[ ! -d "$dest" ]]; then
    echo "  SKIP missing dir"
    return 0
  fi

  local f
  for f in "${LANDLOCK_FILES[@]}"; do
    copy_file "$f" "$dest"
  done
  ensure_gitattributes_landlock "$dest"
  ensure_gitignore_grok_lib "$dest"

  if [[ "$DRY" -eq 0 ]]; then
    if [[ -f "$dest/agent-landlock" ]]; then
      if grep -q '\.grok' "$dest/agent-landlock" 2>/dev/null; then
        echo "  OK agent-landlock has .grok grant"
      else
        echo "  WARN agent-landlock may lack .grok grant string"
      fi
      chmod +x "$dest/agent-landlock" 2>/dev/null || true
    fi
    if [[ -f "$dest/.grok/lib/grok-launch-common.sh" ]]; then
      grep -q 'agent-landlock' "$dest/.grok/lib/grok-launch-common.sh" \
        && echo "  OK launch common wires agent-landlock" \
        || echo "  WARN launch common missing agent-landlock wire"
    else
      echo "  WARN missing .grok/lib/grok-launch-common.sh"
    fi
  fi

  if [[ "$DO_COMMIT" -eq 1 && "$DRY" -eq 0 ]]; then
    commit_target "$dest" "$label"
  fi
}

# ---------- optional VE worktrees ----------
if [[ "$DO_VE" -eq 1 ]]; then
  info "VE worktrees"
  while IFS= read -r wt; do
    [[ -n "$wt" && -d "$wt" ]] || continue
    deploy_to "$wt" "VE $(basename "$wt")"
  done < <(list_worktrees "$SRC")
fi

# ---------- host roots + all worktrees ----------
for host in $HOSTS; do
  root="$GIT_HOME/$host"
  if [[ ! -d "$root" ]]; then
    echo "SKIP missing $root"
    continue
  fi
  info "Host $host — all worktrees"
  # Dedup worktree paths
  declare -A SEEN=()
  while IFS= read -r wt; do
    [[ -n "$wt" && -d "$wt" ]] || continue
    # normalize
    wt="$(cd "$wt" && pwd)"
    [[ -n "${SEEN[$wt]:-}" ]] && continue
    SEEN[$wt]=1
    deploy_to "$wt" "$host/$(basename "$wt")"
  done < <(list_worktrees "$root")
  # Always include root even if worktree list failed
  root_abs="$(cd "$root" && pwd)"
  if [[ -z "${SEEN[$root_abs]:-}" ]]; then
    deploy_to "$root_abs" "$host/$(basename "$root_abs")"
  fi
  unset SEEN
done

echo ""
echo "================================================================"
echo "Landlock deploy done (DRY=$DRY COMMIT=$DO_COMMIT)."
echo "Smoke on a target worktree:"
echo "  cd \$TARGET && ./agent-landlock --status"
echo "  ./agent-landlock --role primary --worktree \"\$PWD\" --dump-grants | grep grok"
echo "  # expect: dir:\$HOME/.grok"
echo "  ./run-grok   # trust-directory / session create should not FS_PERMISSION_DENIED"
echo "Bypass if needed: AGENT_LANDLOCK_DISABLE=1 ./run-grok"
echo "================================================================"
