#!/bin/bash
# publish-work-history.sh — copy this repo's work-history / Grok sandbox
# changes into every sibling project under git_home, and move each
# worktree's own ENGINEERING_LOG.md and TODO.md.
#
# Commits only the paths it changes (git commit --only), as primary_user,
# so a later build_app is not aborted by those dirty tracked files.
# Does not push. Does not run deploy-orchestration. Other dirty paths stay.
#
# A target file is replaced only when its bytes are exactly the pre-change
# blob at BASE (below) or already the new bytes in this working tree.
# Anything else is left untouched and reported. That includes
# VehicleExpenses-automated/grok-worktrees/infra-harden-20260809, whose
# launcher copies diverged: its log stays at the worktree root so the old
# file grants still name real files.
#
# Host-specific files are patched in place (never copied wholesale):
#   .gitattributes  .gitignore  .grok/config.toml  README.md  project-facts.md
#
#   sudo ./publish-work-history.sh --dry-run
#   sudo ./publish-work-history.sh
#
# Old bytes are read from BASE, not from whatever HEAD happens to be, so
# this repo may already contain the same edits when the script commits them.
set -euo pipefail

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# Pre-change orchestration-example master. git show BASE:path is the old blob.
BASE="20d7fe7c446a1598694fc4eeebab28cce51135f1"
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

GIT_HOME="$(sed -n 's/^git_home=//p' "$SRC/project.config" | head -1 | tr -d '\r')"
if [[ -z "$GIT_HOME" || "$GIT_HOME" != /* || ! -d "$GIT_HOME" ]]; then
  echo "ERROR: git_home in $SRC/project.config is not an absolute directory" >&2
  exit 1
fi

git -c "safe.directory=$SRC" -C "$SRC" cat-file -e "$BASE^{commit}" \
  || { echo "ERROR: BASE $BASE is not in $SRC" >&2; exit 1; }
[[ -f "$SRC/agent-landlock" ]] || { echo "ERROR: missing $SRC/agent-landlock" >&2; exit 1; }
grep -q 'worktree / "work-history"' "$SRC/agent-landlock" \
  || { echo "ERROR: source agent-landlock has no work-history grant" >&2; exit 1; }

# Replaced only when the target matches BASE or already matches the source.
# setup_agent.sh and remove_worktree.sh are class root: a checkout that does
# not have them is left without them.
COPY_FILES=(
  .grok/lib/grok-launch-common.sh
  agent-landlock
  todo-close
  append-to-engineering-log
  reset-to-builds.sh
  merge-branch-into-master.sh
  git-merge-drivers/ve-englog
  fix-perms
  setup_agent.sh
  remove_worktree.sh
  perm-model.tsv
  rules.manifest
  landlock.config
  landlock.config.example
)
# Present on some hosts only. Never created.
OPT_FILES=(
  tools/report-orchestration-drift.sh
)
# New file. Created where a checkout already has the shared tools.
NEW_FILES=(
  move-work-history.sh
)

cfg_get() {
  local file="$1" key="$2" line
  [[ -f "$file" ]] || return 0
  line="$(grep -E "^${key}=" "$file" | head -1 || true)"
  printf '%s' "${line#*=}"
}

# Echo the old blob to stdout. Status 1 when BASE has no such path.
show_base() {
  local rel="$1"
  git -c "safe.directory=$SRC" -C "$SRC" cat-file -e "$BASE:$rel" 2>/dev/null || return 1
  git -c "safe.directory=$SRC" -C "$SRC" show "$BASE:$rel"
}

FAILURES=0
note_fail() {
  FAILURES=$((FAILURES + 1))
  echo "ERROR: $*" >&2
}

# Paths this worktree will commit. Reset at the start of each worktree.
TOUCHED=()
mark_path() {
  local p="$1" have
  for have in "${TOUCHED[@]+"${TOUCHED[@]}"}"; do
    [[ "$have" == "$p" ]] && return 0
  done
  TOUCHED+=("$p")
}

# copy_or_skip WT REL CREATE
# CREATE 1: missing target is created. CREATE 0: missing target is skipped.
copy_or_skip() {
  local wt="$1" rel="$2" create="$3"
  local src="$SRC/$rel" dest="$wt/$rel" own mode
  [[ -f "$src" ]] || { note_fail "missing source $rel"; return 1; }
  if [[ ! -e "$dest" && ! -L "$dest" ]]; then
    if [[ "$create" -eq 0 ]]; then
      return 0
    fi
    if [[ -f "$wt/todo-close" ]]; then
      own="$(stat -c '%u:%g' "$wt/todo-close")"
    else
      own="$(stat -c '%u:%g' "$wt")"
    fi
    echo "  create $rel ($own mode 775)"
    mark_path "$rel"
    if [[ "$DRY" -eq 1 ]]; then
      return 0
    fi
    mkdir -p "$(dirname "$dest")"
    cp -- "$src" "$dest"
    chown "$own" "$dest"
    chmod 775 "$dest"
    return 0
  fi
  if [[ -L "$dest" ]]; then
    note_fail "$dest is a symlink; not replacing"
    return 1
  fi
  if cmp -s "$src" "$dest"; then
    echo "  already $rel"
    mark_path "$rel"
    return 0
  fi
  if show_base "$rel" | cmp -s - "$dest"; then
    mode="$(stat -c '%a' "$dest")"
    own="$(stat -c '%u:%g' "$dest")"
    echo "  copy $rel (keep $own mode $mode)"
    mark_path "$rel"
    if [[ "$DRY" -eq 1 ]]; then
      return 0
    fi
    cp -- "$src" "$dest"
    chown "$own" "$dest"
    chmod "$mode" "$dest"
    return 0
  fi
  # The first publish committed these two blobs, then the commit hook rejected
  # a swallowed git error. A checkout that still has that blob is replaced.
  local want="" have
  case "$rel" in
    .grok/lib/grok-launch-common.sh)
      want="c2a76bf7ff3770a6e6f129801ff17fda2639f1f981ed41466380d74b5f248c3d" ;;
    move-work-history.sh)
      want="d5cdf86e0f3b8c059075171998754e1e60e30642e132a6fa4c277c738ee2d49b" ;;
  esac
  have="$(sha256sum "$dest" | awk 'NR==1{print $1}')"
  if [[ -n "$want" && "$have" == "$want" ]]; then
    mode="$(stat -c '%a' "$dest")"
    own="$(stat -c '%u:%g' "$dest")"
    echo "  copy $rel (replace the first publish; keep $own mode $mode)"
    mark_path "$rel"
    if [[ "$DRY" -eq 1 ]]; then
      return 0
    fi
    cp -- "$src" "$dest"
    chown "$own" "$dest"
    chmod "$mode" "$dest"
    return 0
  fi
  echo "  DIVERGED $rel (left unchanged, $(wc -c <"$dest") bytes)"
  return 0
}

# patch_text WT REL KIND
# KIND: facts | readme | config | gitattributes | gitignore
patch_text() {
  local wt="$1"
  local rel="$2"
  local kind="$3"
  local dest="$wt/$rel"
  [[ -f "$dest" ]] || { echo "  no $rel"; return 0; }
  python3 - "$dest" "$kind" "$DRY" <<'PY'
import pathlib, sys
dest, kind, dry = sys.argv[1], sys.argv[2], sys.argv[3] == "1"
path = pathlib.Path(dest)
text = path.read_text(encoding="utf-8")
orig = text
status = "not-updated"

def finish(new, st):
    global text, status
    text, status = new, st

if kind == "facts":
    new_long = (
        "- Launch: `./run-grok-*` via `.grok/lib/grok-launch-common.sh` + a Grok sandbox profile "
        "(`extends=read-only`, generated `.grok/sandbox.toml`). Cursor launchers still use "
        "`agent-landlock`. Planner writes `work-history/` (root `TODO.md` and `ENGINEERING_LOG.md` are symlinks)."
    )
    old_long = "- Launch: `./run-grok-*` via `.grok/lib/grok-launch-common.sh` + `agent-landlock`"
    old_short = "- `./run-grok-*` — role launchers"
    new_short = (
        "- `./run-grok-*` — role launchers. Grok confinement is a sandbox profile "
        "(`extends=read-only`, generated `.grok/sandbox.toml`). Cursor launchers still use "
        "`agent-landlock`. Planner writes `work-history/` (root `TODO.md` and `ENGINEERING_LOG.md` are symlinks)."
    )
    bullet = (
        "- Grok confinement: sandbox profile (`extends=read-only`, generated `.grok/sandbox.toml`). "
        "Cursor launchers still use `agent-landlock`. Planner writes `work-history/` "
        "(root `TODO.md` and `ENGINEERING_LOG.md` are symlinks).\n"
    )
    if "Planner writes `work-history/`" in text:
        finish(text, "already")
    elif text.count(old_long) == 1:
        finish(text.replace(old_long, new_long, 1), "edit")
    elif text.count(old_long) > 1:
        finish(text, "ambiguous")
    elif text.count(old_short) == 1:
        finish(text.replace(old_short, new_short, 1), "edit")
    elif text.count(old_short) > 1:
        finish(text, "ambiguous")
    else:
        lines = text.splitlines(keepends=True)
        hits = [i for i, ln in enumerate(lines) if ln.startswith("- Session Landlock:")]
        if len(hits) == 1:
            lines.insert(hits[0] + 1, bullet)
            finish("".join(lines), "edit")
        elif len(hits) > 1:
            finish(text, "ambiguous")
        else:
            finish(text, "not-updated")
elif kind == "readme":
    old = "Landlock (mutation-only) wraps agents via `agent-landlock` from `run-grok*`."
    new = (
        "Grok launchers confine the process with a custom sandbox profile (`extends=read-only`). "
        "Cursor launchers still wrap with `agent-landlock`."
    )
    if new in text:
        finish(text, "already")
    elif text.count(old) == 1:
        finish(text.replace(old, new, 1), "edit")
    elif text.count(old) > 1:
        finish(text, "ambiguous")
    else:
        finish(text, "not-updated")
elif kind == "config":
    old = "# Role tightening is done by Landlock (agent-landlock) and ROLE_KEY hooks in .grok/hooks/."
    new = (
        "# Role write limits are the Grok sandbox profile (cursor: agent-landlock) and ROLE_KEY hooks in .grok/hooks/.\n"
        "# Launchers pass --always-approve. These deny rules and the hooks still apply."
    )
    if new.split("\n", 1)[0] in text:
        finish(text, "already")
    elif text.count(old) == 1:
        finish(text.replace(old, new, 1), "edit")
    elif text.count(old) > 1:
        finish(text, "ambiguous")
    else:
        finish(text, "not-updated")
elif kind == "gitattributes":
    block = (
        "# After the move the blobs live here; the root paths are symlinks.\n"
        "work-history/ENGINEERING_LOG.md merge=ve-englog\n"
        "work-history/TODO.md merge=ve-special-ours\n"
    )
    if "work-history/ENGINEERING_LOG.md merge=ve-englog" in text:
        finish(text, "already")
    else:
        anchor = "project-facts.md merge=ve-special-ours\n"
        if text.count(anchor) == 0:
            anchor = "TODO.md merge=ve-special-ours\n"
        if text.count(anchor) == 1:
            finish(text.replace(anchor, anchor + block, 1), "edit")
        elif text.count(anchor) > 1:
            finish(text, "ambiguous")
        else:
            finish(text, "not-updated")
elif kind == "gitignore":
    if any(ln.strip() == ".grok/sandbox.toml" for ln in text.splitlines()):
        finish(text, "already")
    else:
        extra = (
            "\n# Generated at launch by .grok/lib/grok-launch-common.sh (role sandbox profile).\n"
            ".grok/sandbox.toml\n"
            ".grok/sandbox.toml.lock\n"
            ".grok/sandbox.toml.tmp\n"
        )
        if text.endswith("\n") or text == "":
            finish(text + extra.lstrip("\n"), "edit")
        else:
            finish(text + extra, "edit")
else:
    raise SystemExit("unknown patch kind %s" % kind)

print(status)
if status == "edit" and not dry and text != orig:
    path.write_text(text, encoding="utf-8")
PY
}

# True when this worktree already has the new planner grant, or the copy
# step will install it (target still equals BASE, or already equals source).
# Dry-run does not copy first, so the BASE check is required for a true plan.
landlock_ready() {
  local wt="$1" dest="$wt/agent-landlock"
  [[ -f "$dest" ]] || return 1
  if grep -q 'worktree / "work-history"' "$dest"; then
    return 0
  fi
  if cmp -s "$SRC/agent-landlock" "$dest"; then
    return 0
  fi
  if show_base agent-landlock | cmp -s - "$dest"; then
    return 0
  fi
  return 1
}

git_owner() {
  local wt="$1"
  shift
  if [[ "$(id -u)" -eq 0 ]]; then
    sudo -u "$GIT_OWNER" git -c "safe.directory=$wt" -c "safe.directory=*" -C "$wt" "$@"
  else
    git -c "safe.directory=$wt" -c "safe.directory=*" -C "$wt" "$@"
  fi
}

# Clear skip-worktree only on tracked paths. An untracked path is not an error.
clear_skip_worktree() {
  local wt="$1"
  shift
  local p tracked
  for p in "$@"; do
    tracked="$(git_owner "$wt" ls-files -- "$p")" || {
      note_fail "git ls-files failed for $p in $wt"
      return 1
    }
    if [[ -n "$tracked" ]]; then
      git_owner "$wt" update-index --no-skip-worktree -- "$p" || {
        note_fail "could not clear skip-worktree on $wt/$p"
        return 1
      }
    fi
  done
}

commit_worktree() {
  local wt="$1"
  local -a paths=()
  local p one line rest n
  if [[ "$wt" == "$SRC" && -f "$wt/publish-work-history.sh" ]]; then
    mark_path "publish-work-history.sh"
  fi
  if [[ ${#TOUCHED[@]} -eq 0 ]]; then
    echo "  commit: nothing from this script"
    return 0
  fi
  echo "  commit these paths:"
  for p in "${TOUCHED[@]}"; do
    echo "    $p"
  done
  if [[ "$DRY" -eq 1 ]]; then
    return 0
  fi
  for p in "${TOUCHED[@]}"; do
    [[ -e "$wt/$p" || -L "$wt/$p" ]] || continue
    one="$(git_owner "$wt" status --porcelain -- "$p")" \
      || { note_fail "git status failed for $wt/$p"; return 1; }
    if [[ -n "$one" ]]; then
      paths+=("$p")
    fi
  done
  if [[ ${#paths[@]} -eq 0 ]]; then
    echo "  commit: those paths already match HEAD"
    return 0
  fi
  clear_skip_worktree "$wt" "${paths[@]}" \
    || { note_fail "skip-worktree clear failed in $wt"; return 1; }
  git_owner "$wt" add -f -- "${paths[@]}" \
    || { note_fail "git add failed in $wt"; return 1; }
  # --only commits just these paths. Other staged or unstaged work stays.
  local moves_log=0
  for p in "${paths[@]}"; do
    case "$p" in
      TODO.md|ENGINEERING_LOG.md|work-history/TODO.md|work-history/ENGINEERING_LOG.md)
        moves_log=1 ;;
    esac
  done
  if [[ "$moves_log" -eq 1 && -L "$wt/TODO.md" && -L "$wt/ENGINEERING_LOG.md" ]]; then
    git_owner "$wt" commit --only \
      -m "chore: move TODO and the engineering log into work-history" \
      -m "Grok launchers use a read-only sandbox profile. Cursor still uses agent-landlock. Root TODO.md and ENGINEERING_LOG.md are symlinks into work-history/." \
      -- "${paths[@]}" \
      || { note_fail "git commit failed in $wt"; return 1; }
  elif [[ "$moves_log" -eq 1 ]]; then
    git_owner "$wt" commit --only \
      -m "chore: Grok sandbox profile tooling" \
      -m "TODO.md and ENGINEERING_LOG.md stayed at the worktree root because this checkout's agent-landlock was not updated." \
      -- "${paths[@]}" \
      || { note_fail "git commit failed in $wt"; return 1; }
  else
    git_owner "$wt" commit --only \
      -m "fix: report git errors in the sandbox launch and the work-history move" \
      -m "A failed git rev-parse or update-index is reported. Skip-worktree is cleared only when the path is tracked." \
      -- "${paths[@]}" \
      || { note_fail "git commit failed in $wt"; return 1; }
  fi
  echo "  committed $(git_owner "$wt" rev-parse --short HEAD) on $(git_owner "$wt" branch --show-current)"
  rest="$(git_owner "$wt" status --porcelain -uno || true)"
  n=0
  if [[ -n "$rest" ]]; then
    while IFS= read -r line; do
      [[ -n "$line" ]] && n=$((n + 1))
    done <<<"$rest"
  fi
  if [[ "$n" -gt 0 ]]; then
    echo "  note: $n other tracked path(s) still dirty; build_app will still abort until those are committed"
  fi
}

RESEAL=()
reseal() {
  local f
  for f in "${RESEAL[@]+"${RESEAL[@]}"}"; do
    chattr +a -- "$f" || echo "ERROR: could not restore chattr +a on $f" >&2
  done
}

move_tracked() {
  local wt="$1" name="$2" seal="$3"
  local src="$wt/$name"
  local dest="$wt/work-history/$name"
  if [[ -L "$src" ]]; then
    local target
    target="$(readlink -- "$src")"
    if [[ "$target" == "work-history/$name" && -f "$dest" ]]; then
      echo "  $name already links to work-history/$name"
      mark_path "$name"
      mark_path "work-history/$name"
      if [[ "$seal" -eq 1 && "$DRY" -eq 0 ]]; then
        chattr +a -- "$dest"
      fi
      return 0
    fi
    note_fail "$src is a symlink to '$target', not work-history/$name"
    return 1
  fi
  if [[ ! -e "$src" && ! -L "$src" && -f "$dest" ]]; then
    echo "  $name is already in work-history/; adding the symlink"
    mark_path "$name"
    mark_path "work-history/$name"
    if [[ "$DRY" -eq 1 ]]; then
      return 0
    fi
    ln -s "work-history/$name" "$src"
    chown -h "$GIT_OWNER:$SHARED" "$src" 2>/dev/null || chown -h "$GIT_OWNER" "$src"
    git_owner "$wt" add -- "$name" || { note_fail "git add $name failed in $wt"; return 1; }
    if [[ "$seal" -eq 1 ]]; then
      chattr +a -- "$dest"
    fi
    return 0
  fi
  if [[ ! -f "$src" ]]; then
    note_fail "$src is not a regular file"
    return 1
  fi
  if [[ -e "$dest" ]]; then
    note_fail "$dest already exists; not overwriting"
    return 1
  fi
  local attrs="" need_seal=0
  attrs="$(lsattr -d -- "$src")" || { note_fail "lsattr $src failed"; return 1; }
  case "${attrs%% *}" in
    *a*) need_seal=1 ;;
  esac
  echo "  move $name -> work-history/$name (append-only=$need_seal)"
  mark_path "$name"
  mark_path "work-history/$name"
  if [[ "$DRY" -eq 1 ]]; then
    return 0
  fi
  if [[ "$need_seal" -eq 1 ]]; then
    chattr -a -- "$src"
    RESEAL+=("$src")
  fi
  clear_skip_worktree "$wt" "$name" || return 1
  if ! git_owner "$wt" mv -- "$name" "work-history/$name"; then
    note_fail "git mv $name failed in $wt"
    return 1
  fi
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
  chown -h "$GIT_OWNER:$SHARED" "$src" 2>/dev/null || chown -h "$GIT_OWNER" "$src"
  if ! git_owner "$wt" add -- "$name"; then
    note_fail "git add $name failed in $wt"
    return 1
  fi
  if [[ "$need_seal" -eq 1 ]]; then
    chattr +a -- "$dest"
    local keep2=() g2
    for g2 in "${RESEAL[@]+"${RESEAL[@]}"}"; do
      [[ "$g2" == "$dest" ]] || keep2+=("$g2")
    done
    RESEAL=("${keep2[@]+"${keep2[@]}"}")
  fi
}

list_worktrees() {
  local root="$1"
  git -c "safe.directory=$root" -c "safe.directory=*" -C "$root" worktree list --porcelain \
    | awk '/^worktree / {print substr($0, 10)}'
}

echo "Source: $SRC"
echo "BASE: $BASE"
echo "GIT_HOME: $GIT_HOME"
echo "dry-run: $DRY"
echo "Commits only the paths this script changes. Does not push."

shopt -s nullglob
ROOTS=()
for d in "$GIT_HOME"/*/; do
  if [[ -f "${d}setup_agent.sh" || -f "${d}.grok/lib/grok-launch-common.sh" ]]; then
    ROOTS+=("${d%/}")
  fi
done
shopt -u nullglob

if [[ ${#ROOTS[@]} -eq 0 ]]; then
  echo "ERROR: no host checkouts under $GIT_HOME" >&2
  exit 1
fi

for root in "${ROOTS[@]}"; do
  echo ""
  echo "======== $root"
  if ! git -c "safe.directory=$root" -C "$root" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    note_fail "$root is not a git worktree"
    continue
  fi
  GIT_OWNER="$(cfg_get "$root/project.config" primary_user)"
  SHARED="$(cfg_get "$root/project.config" shared_group)"
  if [[ -z "$GIT_OWNER" ]]; then
    GIT_OWNER="$(stat -c '%U' "$root")"
    echo "  note: no primary_user in project.config; using directory owner $GIT_OWNER"
  fi
  if [[ -z "$SHARED" ]]; then
    # project.config is gitignored. paddle-build-test has no copy in this checkout.
    SHARED="$(cfg_get "$SRC/project.config" shared_group)"
    echo "  note: no shared_group in $root/project.config; using $SHARED"
  fi
  if [[ -z "$GIT_OWNER" || -z "$SHARED" ]]; then
    note_fail "$root: git owner or shared group is empty"
    continue
  fi
  if [[ "$DRY" -eq 0 ]] && ! id "$GIT_OWNER" >/dev/null 2>&1; then
    note_fail "$root: user $GIT_OWNER does not exist"
    continue
  fi
  echo "Git owner: $GIT_OWNER  shared group: $SHARED"

  mapfile -t WTS < <(list_worktrees "$root")
  if [[ ${#WTS[@]} -eq 0 ]]; then
    note_fail "$root: no worktrees"
    continue
  fi

  for wt in "${WTS[@]}"; do
    echo "Worktree: $wt"
    fails_before="$FAILURES"
    TOUCHED=()
    for rel in "${COPY_FILES[@]}"; do
      copy_or_skip "$wt" "$rel" 0 || true
    done
    for rel in "${OPT_FILES[@]}"; do
      if [[ -f "$wt/$rel" ]]; then
        copy_or_skip "$wt" "$rel" 0 || true
      fi
    done
    if [[ -f "$wt/todo-close" ]]; then
      copy_or_skip "$wt" "move-work-history.sh" 1 || true
    fi

    for spec in readme:README.md config:.grok/config.toml gitattributes:.gitattributes gitignore:.gitignore; do
      kind="${spec%%:*}"
      rel="${spec#*:}"
      st="$(patch_text "$wt" "$rel" "$kind")" || { note_fail "patch $rel in $wt"; continue; }
      echo "  $st $rel"
      if [[ "$st" == "edit" || "$st" == "already" ]]; then
        mark_path "$rel"
      fi
      if [[ "$st" == "ambiguous" ]]; then
        note_fail "$wt: $rel patch is ambiguous"
      elif [[ "$kind" == "gitattributes" && "$st" != "edit" && "$st" != "already" ]]; then
        note_fail "$wt: .gitattributes was not updated ($st)"
      fi
    done

    if landlock_ready "$wt" || { [[ -L "$wt/TODO.md" && -L "$wt/ENGINEERING_LOG.md" ]]; }; then
      st="$(patch_text "$wt" "project-facts.md" "facts")" || { note_fail "patch project-facts.md in $wt"; st="fail"; }
      echo "  $st project-facts.md"
      if [[ "$st" == "edit" || "$st" == "already" ]]; then
        mark_path "project-facts.md"
      fi
      if [[ "$st" != "edit" && "$st" != "already" ]]; then
        note_fail "$wt: project-facts.md was $st"
      fi
      if [[ "$DRY" -eq 1 ]]; then
        echo "  mkdir -p $wt/work-history && chown $GIT_OWNER:$SHARED && chmod 2775"
      else
        mkdir -p "$wt/work-history"
        chown "$GIT_OWNER:$SHARED" "$wt/work-history"
        chmod 2775 "$wt/work-history"
      fi
      RESEAL=()
      trap reseal EXIT
      move_tracked "$wt" ENGINEERING_LOG.md 1 || true
      move_tracked "$wt" TODO.md 0 || true
      if [[ ${#RESEAL[@]} -gt 0 ]]; then
        note_fail "$wt: append-only still lifted on ${RESEAL[*]}"
        reseal
      fi
      RESEAL=()
      trap - EXIT
    else
      echo "  LEFT IN PLACE: ENGINEERING_LOG.md and TODO.md (agent-landlock was not updated)"
      echo "  not-updated project-facts.md (layout was not moved here)"
    fi
    if [[ "$FAILURES" -gt "$fails_before" ]]; then
      echo "  commit skipped; this worktree hit an error"
    else
      commit_worktree "$wt" || true
    fi
  done
done

echo ""
if [[ "$FAILURES" -eq 0 ]]; then
  echo "Done. Each worktree with changes was committed. Not pushed."
  exit 0
fi
echo "Done with $FAILURES error(s). Not pushed." >&2
exit 1
