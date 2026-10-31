#!/bin/bash
# publish-revert-grok-launch.sh — put grok launch back on agent-landlock.
#
# The run-grok* scripts did not change. They source
# .grok/lib/grok-launch-common.sh, and that file is what started passing
# --sandbox on a generated read-only profile. This restores that library
# to 20d7fe7c (agent-landlock, --sandbox only when GROK_SANDBOX is set).
# Where project-facts.md has the sandbox-profile launch sentence, that
# sentence is pointed back at agent-landlock.
#
# Replaces the library only when its bytes are the published sandbox
# version or the pre-profile version. A diverged copy is left alone.
# Commits only those paths (and this script, in this repo) with
# git commit --only, as primary_user. Does not push.
#
#   sudo ./publish-revert-grok-launch.sh --dry-run
#   sudo ./publish-revert-grok-launch.sh
set -euo pipefail

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE=20d7fe7c446a1598694fc4eeebab28cce51135f1
REL=.grok/lib/grok-launch-common.sh
DRY=0
parsed="$(getopt -o hn -l help,dry-run -- "$@")" || exit 2
eval set -- "$parsed"
while true; do
  case "$1" in
    -h|--help) echo "usage: sudo $0 [-n|--dry-run]"; exit 0 ;;
    -n|--dry-run) DRY=1; shift ;;
    --) shift; break ;;
    *) echo "usage: sudo $0 [-n|--dry-run]" >&2; exit 2 ;;
  esac
done
if [[ $# -gt 0 ]]; then
  echo "usage: sudo $0 [-n|--dry-run]" >&2
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

TARGET_FILE="$(mktemp)"
trap 'rm -f "$TARGET_FILE"' EXIT
git -c "safe.directory=$SRC" -C "$SRC" show "$BASE:$REL" > "$TARGET_FILE"
if [[ ! -s "$TARGET_FILE" ]]; then
  echo "ERROR: $BASE:$REL is empty" >&2
  exit 1
fi

FAILURES=0
note_fail() {
  FAILURES=$((FAILURES + 1))
  echo "ERROR: $*" >&2
}

cfg_get() {
  local file="$1" key="$2" line
  [[ -f "$file" ]] || return 0
  line="$(grep -E "^${key}=" "$file" | head -1 || true)"
  printf '%s' "${line#*=}"
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

# Print already, edit, missing, or diverged.
patch_launcher() {
  local wt="$1"
  python3 - "$wt/$REL" "$DRY" "$TARGET_FILE" <<'PY'
import os, stat, sys
path, dry, target_path = sys.argv[1], sys.argv[2] == "1", sys.argv[3]
target = open(target_path, "rb").read()
# Published sandbox launcher, then the first-publish copy from before the
# swallow-line fix. Both are replaced by the pre-profile launcher.
replace = {
    "e3bd15667e641c484dc0bdce93e99ea4789ec2ae3f336c3c4082a470107a70d9",
    "c2a76bf7ff3770a6e6f129801ff17fda2639f1f981ed41466380d74b5f248c3d",
}
import hashlib
if not os.path.isfile(path):
    print("missing")
    raise SystemExit(0)
data = open(path, "rb").read()
digest = hashlib.sha256(data).hexdigest()
if data == target:
    print("already")
    raise SystemExit(0)
if digest not in replace:
    print("diverged")
    raise SystemExit(0)
print("edit")
if dry:
    raise SystemExit(0)
st = os.stat(path)
tmp = path + ".launchrevert.tmp"
with open(tmp, "wb") as f:
    f.write(target)
    f.flush()
    os.fsync(f.fileno())
os.chmod(tmp, stat.S_IMODE(st.st_mode))
os.chown(tmp, st.st_uid, st.st_gid)
os.replace(tmp, path)
PY
}

# Print edit, already, absent, or skipped.
patch_facts() {
  local wt="$1"
  python3 - "$wt/project-facts.md" "$DRY" <<'PY'
import os, stat, sys
path, dry = sys.argv[1], sys.argv[2] == "1"
old = "+ a Grok sandbox profile (`extends=read-only`, generated `.grok/sandbox.toml`). Cursor launchers still use `agent-landlock`."
new = "+ `agent-landlock` (Grok and cursor)."
if not os.path.isfile(path):
    print("absent")
    raise SystemExit(0)
text = open(path, encoding="utf-8", errors="replace").read()
if old not in text:
    if new in text:
        print("already")
    else:
        print("skipped")
    raise SystemExit(0)
print("edit")
if dry:
    raise SystemExit(0)
updated = text.replace(old, new, 1)
st = os.stat(path)
tmp = path + ".factsrevert.tmp"
with open(tmp, "w", encoding="utf-8") as f:
    f.write(updated)
    f.flush()
    os.fsync(f.fileno())
os.chmod(tmp, stat.S_IMODE(st.st_mode))
os.chown(tmp, st.st_uid, st.st_gid)
os.replace(tmp, path)
PY
}

commit_revert() {
  local wt="$1"
  local -a paths=()
  local p one rest n line
  local -a candidates=("$REL" project-facts.md)
  if [[ "$wt" == "$SRC" ]]; then
    candidates+=(publish-revert-grok-launch.sh)
  fi
  if [[ "$DRY" -eq 1 ]]; then
    echo "  would commit: ${candidates[*]}"
    return 0
  fi
  for p in "${candidates[@]}"; do
    one="$(git_owner "$wt" status --porcelain -- "$p")" || {
      note_fail "git status failed for $wt/$p"
      return 1
    }
    if [[ -n "$one" ]]; then
      paths+=("$p")
    fi
  done
  if [[ ${#paths[@]} -eq 0 ]]; then
    echo "  commit: launcher already matches HEAD"
    return 0
  fi
  echo "  commit: ${paths[*]}"
  clear_skip_worktree "$wt" "${paths[@]}" || return 1
  git_owner "$wt" add -f -- "${paths[@]}" || {
    note_fail "git add failed in $wt"
    return 1
  }
  git_owner "$wt" commit --only \
    -m "fix: launch grok under agent-landlock again" \
    -m "The generated read-only Grok sandbox profile starts bubblewrap, which keeps only the primary group. build_app then reports the shell is missing ai-shared. run-grok* again uses agent-landlock and passes --sandbox only when GROK_SANDBOX is set." \
    -- "${paths[@]}" || {
    note_fail "git commit failed in $wt"
    return 1
  }
  echo "  committed $(git_owner "$wt" rev-parse --short HEAD) on $(git_owner "$wt" branch --show-current)"
  if ! rest="$(git_owner "$wt" status --porcelain -uno)"; then
    echo "  note: could not list remaining dirty paths"
    return 0
  fi
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

list_worktrees() {
  local root="$1"
  git -c "safe.directory=$root" -c "safe.directory=*" -C "$root" worktree list --porcelain \
    | awk '/^worktree / {print substr($0, 10)}'
}

echo "Source: $SRC"
echo "Restores $REL from $BASE"
echo "dry-run: $DRY"
echo "The run-grok* scripts are unchanged and are not copied."
echo "Commits the launcher and a matching project-facts.md sentence. Does not push."

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
  if [[ -z "$GIT_OWNER" ]]; then
    GIT_OWNER="$(stat -c '%U' "$root")"
    echo "  note: no primary_user in project.config; using directory owner $GIT_OWNER"
  fi
  if [[ "$DRY" -eq 0 ]] && ! id "$GIT_OWNER" >/dev/null 2>&1; then
    note_fail "$root: user $GIT_OWNER does not exist"
    continue
  fi
  echo "Git owner: $GIT_OWNER"
  mapfile -t WTS < <(list_worktrees "$root")
  if [[ ${#WTS[@]} -eq 0 ]]; then
    note_fail "$root: no worktrees"
    continue
  fi
  for wt in "${WTS[@]}"; do
    echo "Worktree: $wt"
    fails_before="$FAILURES"
    st="$(patch_launcher "$wt")" || { note_fail "patch launcher in $wt"; continue; }
    echo "  $st $REL"
    case "$st" in
      edit|already) ;;
      missing)
        echo "  no launcher; left unchanged"
        continue
        ;;
      *)
        echo "  note: launcher left unchanged ($st)"
        continue
        ;;
    esac
    fst="$(patch_facts "$wt")" || { note_fail "patch facts in $wt"; continue; }
    echo "  $fst project-facts.md"
    if [[ "$FAILURES" -gt "$fails_before" ]]; then
      echo "  skip commit"
      continue
    fi
    if [[ "$DRY" -eq 1 ]]; then
      echo "  would commit $REL if it differs from HEAD"
      if [[ "$fst" == edit ]]; then
        echo "  would commit project-facts.md"
      fi
      if [[ "$wt" == "$SRC" ]]; then
        echo "  would commit publish-revert-grok-launch.sh"
      fi
      continue
    fi
    commit_revert "$wt" || true
  done
done

echo ""
if [[ "$FAILURES" -gt 0 ]]; then
  echo "Done with $FAILURES error(s). Not pushed."
  exit 1
fi
echo "Done. Not pushed."
