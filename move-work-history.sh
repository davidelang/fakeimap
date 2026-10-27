#!/bin/bash
# move-work-history.sh — root-only layout move, plus the helper edits this
# user cannot write (mode 644/755, not group-writable).
#
# This script is for the repo it lives in. It does not walk other projects.
# It does not commit.
#
#   sudo ./move-work-history.sh           # apply
#   sudo ./move-work-history.sh --dry-run # print only
#
# For each worktree of this repo:
#   work-history/ is 2775 and group shared_group (planner can create a temp)
#   ENGINEERING_LOG.md and TODO.md move there
#   the old paths become relative symlinks
#   chattr +a is put back on the real log
# Git index is updated (git mv + git add of the symlink) as SUDO_USER.
# Append-only is lifted only for that mv, and restored if a later step fails.
set -euo pipefail

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DRY=0
if [[ "${1:-}" == "--dry-run" ]]; then
  DRY=1
elif [[ -n "${1:-}" ]]; then
  echo "usage: sudo $0 [--dry-run]" >&2
  exit 2
fi

if [[ "$DRY" -eq 0 && "$(id -u)" -ne 0 ]]; then
  echo "ERROR: run as root: sudo $0" >&2
  exit 1
fi

cfg_get() {
  local key="$1" line
  line="$(grep -E "^${key}=" "$SRC/project.config" | head -1 || true)"
  printf '%s' "${line#*=}"
}

OWNER="${SUDO_USER:-}"
if [[ -z "$OWNER" || "$OWNER" == root ]]; then
  OWNER="$(cfg_get primary_user)"
fi
SHARED="$(cfg_get shared_group)"
if [[ -z "$OWNER" || -z "$SHARED" ]]; then
  echo "ERROR: primary_user or shared_group missing in $SRC/project.config" >&2
  exit 1
fi
if ! id "$OWNER" >/dev/null 2>&1; then
  echo "ERROR: user $OWNER does not exist" >&2
  exit 1
fi

echo "Repo: $SRC"
echo "Git owner: $OWNER  shared group: $SHARED  dry-run: $DRY"

python3 - "$SRC" "$DRY" <<'PY'
import pathlib, sys
root = pathlib.Path(sys.argv[1])
dry = sys.argv[2] == "1"

def edit(rel, old, new):
    path = root / rel
    if not path.is_file():
        raise SystemExit("missing %s" % path)
    text = path.read_text(encoding="utf-8")
    if old == new:
        raise SystemExit("empty change for %s" % rel)
    if old not in text:
        if new in text:
            print("  already: %s" % rel)
            return
        raise SystemExit("pattern not found in %s" % rel)
    if text.count(old) != 1:
        raise SystemExit("pattern found %d times in %s" % (text.count(old), rel))
    print("  edit: %s" % rel)
    if dry:
        return
    path.write_text(text.replace(old, new, 1), encoding="utf-8")

print("Helper edits:")
edit(
    "project-facts.md",
    "- Launch: `./run-grok-*` via `.grok/lib/grok-launch-common.sh` + `agent-landlock`",
    "- Launch: `./run-grok-*` via `.grok/lib/grok-launch-common.sh` + a Grok sandbox profile (`extends=read-only`, generated `.grok/sandbox.toml`). Cursor launchers still use `agent-landlock`. Planner writes `work-history/` (root `TODO.md` and `ENGINEERING_LOG.md` are symlinks).",
)
edit(
    "landlock.config",
    """# landlock.config: extra grants for agent-landlock (profile paths, *_paths= lists).
# git_home, orch_root and sandbox_dir come from project.config only. No secrets here.
# Use absolute paths or env:VAR/... ; a grant that is empty or holds @@ fails.
# agent-landlock reads role grants from worktree + this file + project.config.""",
    """# landlock.config: extra directory grants (profile paths, *_paths= lists).
# git_home, orch_root and sandbox_dir come from project.config only. No secrets here.
# Use absolute paths or env:VAR/... ; a grant that is empty or holds @@ fails.
# Cursor: agent-landlock reads this. Grok: grok-launch-common.sh reads the same
# profile.* path= lines into the role sandbox (env:HOME is that role user's home).""",
)
edit(
    "landlock.config",
    "# planner: sandbox + worktree TODO.md + ENGINEERING_LOG.md (files)",
    "# planner: sandbox + work-history/ (TODO.md and ENGINEERING_LOG.md live there)",
)
edit(
    "landlock.config.example",
    """# landlock.config.example: reference twin of landlock.config (same grants).
# git_home, orch_root and sandbox_dir come from project.config only.""",
    """# landlock.config.example: reference twin of landlock.config (same grants).
# git_home, orch_root and sandbox_dir come from project.config only.
# Cursor (agent-landlock) and Grok (role sandbox profile) both read profile.* path=.""",
)
edit(
    ".grok/config.toml",
    "# Role tightening is done by Landlock (agent-landlock) and ROLE_KEY hooks in .grok/hooks/.",
    "# Role write limits are the Grok sandbox profile (cursor: agent-landlock) and ROLE_KEY hooks in .grok/hooks/.\n# Launchers pass --always-approve. These deny rules and the hooks still apply.",
)
edit(
    "rules.manifest",
    "shared todo-close\n",
    "shared todo-close\nshared move-work-history.sh\n",
)
edit(
    "perm-model.tsv",
    "planner-files\t-\tshared_group\t-\t664\t002\tplanner\tTODO.md and project-facts.md in a worktree\n",
    "planner-files\t-\tshared_group\t-\t664\t002\tplanner\tTODO.md and project-facts.md in a worktree\nwork-history\t-\tshared_group\t2775\t664\t002\tplanner\twork-history/ directory (TODO and the log live here; the log file is then reset by englog)\n",
)
edit(
    "fix-perms",
    """lift_log() {
  chattr -a "$1"
  LIFTED+=("$1")
}""",
    """# chattr does not follow a symlink. The root log name links into work-history/.
lift_log() {
  local real
  real=$(readlink -f -- "$1")
  chattr -a "$real"
  LIFTED+=("$real")
}""",
)
edit(
    "fix-perms",
    """  for f in TODO.md project-facts.md; do
    if [ -f "$wt/$f" ]; then
      perm_apply planner-files "$wt/$f" --top-only
    fi
  done""",
    """  for f in TODO.md project-facts.md; do
    if [ -e "$wt/$f" ] || [ -L "$wt/$f" ]; then
      perm_apply planner-files "$(readlink -f -- "$wt/$f")" --top-only
    fi
  done
  if [ -d "$wt/work-history" ]; then
    perm_apply work-history "$wt/work-history"
  fi""",
)
edit(
    "fix-perms",
    """  LOG="$wt/ENGINEERING_LOG.md"
  case " ${LIFTED[*]} " in
    *" $LOG "*) ;;
    *) lift_log "$LOG" ;;
  esac""",
    """  LOG=$(readlink -f -- "$wt/ENGINEERING_LOG.md")
  case " ${LIFTED[*]} " in
    *" $LOG "*) ;;
    *) lift_log "$LOG" ;;
  esac""",
)
edit(
    "setup_agent.sh",
    """  if [ -f "agent-$N/ENGINEERING_LOG.md" ]; then
    sudo chattr -a "agent-$N/ENGINEERING_LOG.md" || _setup_die "cannot lift chattr +a on agent-$N/ENGINEERING_LOG.md"
  fi""",
    """  if [ -e "agent-$N/ENGINEERING_LOG.md" ] || [ -L "agent-$N/ENGINEERING_LOG.md" ]; then
    _real=$(readlink -f -- "agent-$N/ENGINEERING_LOG.md")
    sudo chattr -a -- "$_real" || _setup_die "cannot lift chattr +a on $_real"
  fi""",
)
edit(
    "setup_agent.sh",
    """if [ ! -f "$AGENT_ABS/ENGINEERING_LOG.md" ]; then
  echo "## $(date +%Y-%m-%d) - Initial log for $AGENT_ID" > "$AGENT_ABS/ENGINEERING_LOG.md" \\
    || _setup_die "cannot create $AGENT_ABS/ENGINEERING_LOG.md"
fi""",
    """mkdir -p "$AGENT_ABS/work-history" || _setup_die "cannot create $AGENT_ABS/work-history"
if [ ! -e "$AGENT_ABS/work-history/ENGINEERING_LOG.md" ]; then
  if [ -f "$AGENT_ABS/ENGINEERING_LOG.md" ] && [ ! -L "$AGENT_ABS/ENGINEERING_LOG.md" ]; then
    : # still at the worktree root; move-work-history.sh relocates it
  else
    echo "## $(date +%Y-%m-%d) - Initial log for $AGENT_ID" > "$AGENT_ABS/work-history/ENGINEERING_LOG.md" \\
      || _setup_die "cannot create $AGENT_ABS/work-history/ENGINEERING_LOG.md"
    ln -sfn work-history/ENGINEERING_LOG.md "$AGENT_ABS/ENGINEERING_LOG.md" \\
      || _setup_die "cannot link $AGENT_ABS/ENGINEERING_LOG.md"
  fi
elif [ ! -e "$AGENT_ABS/ENGINEERING_LOG.md" ]; then
  ln -sfn work-history/ENGINEERING_LOG.md "$AGENT_ABS/ENGINEERING_LOG.md" \\
    || _setup_die "cannot link $AGENT_ABS/ENGINEERING_LOG.md"
fi""",
)
edit(
    "remove_worktree.sh",
    """_log="$WORKTREE_DIR/ENGINEERING_LOG.md"
if [ -e "$_log" ] && [ ! -L "$_log" ]; then
    _attrs=$(lsattr -d -- "$_log") || {
        echo "Error: lsattr failed on $_log; cannot tell whether it is append-only." >&2; exit 1; }
    case "${_attrs%% *}" in
        *a*)
            echo "  $_log is append-only: sudo chattr -a"
            sudo chattr -a -- "$_log" || {
                echo "Error: sudo chattr -a $_log failed; the worktree cannot be removed." >&2; exit 1; }
            ;;
    esac
fi""",
    """_log="$WORKTREE_DIR/ENGINEERING_LOG.md"
if [ -e "$_log" ] || [ -L "$_log" ]; then
    _real=$(readlink -f -- "$_log")
    if [ -f "$_real" ]; then
        _attrs=$(lsattr -d -- "$_real") || {
            echo "Error: lsattr failed on $_real; cannot tell whether it is append-only." >&2; exit 1; }
        case "${_attrs%% *}" in
            *a*)
                echo "  $_real is append-only: sudo chattr -a"
                sudo chattr -a -- "$_real" || {
                    echo "Error: sudo chattr -a $_real failed; the worktree cannot be removed." >&2; exit 1; }
                ;;
        esac
    fi
fi""",
)
print("Helper edits done.")
PY

# git as the tree owner when we are root, so the index stays theirs.
git_owner() {
  local wt="$1"
  shift
  if [[ "$(id -u)" -eq 0 ]]; then
    sudo -u "$OWNER" git -c safe.directory="$wt" -C "$wt" "$@"
  else
    git -c safe.directory="$wt" -C "$wt" "$@"
  fi
}

# Clear skip-worktree only when the path is tracked. Untracked is not an error.
clear_skip_worktree() {
  local wt="$1" name="$2" tracked
  tracked="$(git_owner "$wt" ls-files -- "$name")" || {
    echo "ERROR: git ls-files failed for $name in $wt" >&2
    exit 1
  }
  if [[ -n "$tracked" ]]; then
    git_owner "$wt" update-index --no-skip-worktree -- "$name" || {
      echo "ERROR: could not clear skip-worktree on $wt/$name" >&2
      exit 1
    }
  fi
}

# --- move the two files in every worktree of this repo ---
mapfile -t WORKTREES < <(git_owner "$SRC" worktree list --porcelain | awk '/^worktree / {print $2}')
if [[ ${#WORKTREES[@]} -eq 0 ]]; then
  echo "ERROR: no worktrees" >&2
  exit 1
fi

RESEAL=()
reseal() {
  local f
  for f in "${RESEAL[@]+"${RESEAL[@]}"}"; do
    chattr +a -- "$f" || echo "ERROR: could not restore chattr +a on $f" >&2
  done
}
trap reseal EXIT

move_tracked() {
  local wt="$1" name="$2" seal="$3"
  local src="$wt/$name"
  local dest="$wt/work-history/$name"
  if [[ -L "$src" ]]; then
    local target
    target="$(readlink -- "$src")"
    if [[ "$target" == "work-history/$name" && -f "$dest" ]]; then
      echo "  $wt: $name already links to work-history/$name"
      if [[ "$seal" -eq 1 && "$DRY" -eq 0 ]]; then
        chattr +a -- "$dest"
      fi
      return 0
    fi
    echo "ERROR: $src is a symlink to '$target', not work-history/$name" >&2
    exit 1
  fi
  if [[ ! -f "$src" ]]; then
    echo "ERROR: $src is not a regular file" >&2
    exit 1
  fi
  if [[ -e "$dest" ]]; then
    echo "ERROR: $dest already exists; not overwriting" >&2
    exit 1
  fi
  local attrs=""
  attrs="$(lsattr -d -- "$src")" || { echo "ERROR: lsattr $src failed" >&2; exit 1; }
  local need_seal=0
  case "${attrs%% *}" in
    *a*) need_seal=1 ;;
  esac
  echo "  $wt: move $name -> work-history/$name (append-only=$need_seal)"
  if [[ "$DRY" -eq 1 ]]; then
    return 0
  fi
  if [[ "$need_seal" -eq 1 ]]; then
    chattr -a -- "$src"
    RESEAL+=("$src")
  fi
  clear_skip_worktree "$wt" "$name"
  git_owner "$wt" mv -- "$name" "work-history/$name"
  # The inode now lives at dest. A later failure must reseal that path.
  if [[ "$need_seal" -eq 1 ]]; then
    local keep=() g
    for g in "${RESEAL[@]+"${RESEAL[@]}"}"; do
      if [[ "$g" == "$src" ]]; then
        keep+=("$dest")
      else
        keep+=("$g")
      fi
    done
    RESEAL=("${keep[@]+"${keep[@]}"}")
  fi
  ln -s "work-history/$name" "$src"
  chown -h "$OWNER:$SHARED" "$src" 2>/dev/null || chown -h "$OWNER" "$src"
  git_owner "$wt" add -- "$name"
  if [[ "$need_seal" -eq 1 ]]; then
    chattr +a -- "$dest"
    local keep2=() g2
    for g2 in "${RESEAL[@]+"${RESEAL[@]}"}"; do
      [[ "$g2" == "$dest" ]] || keep2+=("$g2")
    done
    RESEAL=("${keep2[@]+"${keep2[@]}"}")
  fi
}

for wt in "${WORKTREES[@]}"; do
  echo "Worktree: $wt"
  if [[ "$DRY" -eq 1 ]]; then
    echo "  mkdir -p $wt/work-history && chown $OWNER:$SHARED && chmod 2775"
  else
    mkdir -p "$wt/work-history"
    chown "$OWNER:$SHARED" "$wt/work-history"
    chmod 2775 "$wt/work-history"
  fi
  move_tracked "$wt" ENGINEERING_LOG.md 1
  move_tracked "$wt" TODO.md 0
done

trap - EXIT
echo "Done. Not committed. Review with: sudo -u $OWNER git -C $SRC status --short"
