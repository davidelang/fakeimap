#!/bin/bash
# publish-env-fix.sh — fix env's project.config loader in every sibling
# worktree and commit that file.
#
# The old loader ran sed before grep, so primary_user=dlang became
# "export primary_user dlang" and then failed the grep. source ./env
# stopped with "primary_user is unset" before it could refresh groups.
#
# Replaces that loader only. The rest of each checkout's env stays.
# Commits only env (and this script, in this repo) with git commit --only,
# as primary_user. Does not push.
#
#   sudo ./publish-env-fix.sh --dry-run
#   sudo ./publish-env-fix.sh
set -euo pipefail

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DRY=0
USAGE_LINE="usage: ./publish-env-fix.sh [-h] [-n]"
usage() {
  cat <<EOF
$USAGE_LINE

Fix env's project.config loader in every sibling worktree and commit it (root only).

options:
  -h, --help     show this help message and exit
  -n, --dry-run  print what would change and change nothing

Details: the comment at the top of ./publish-env-fix.sh.
EOF
}
parsed="$(getopt -n publish-env-fix.sh -o hn -l help,dry-run -- "$@")" || { echo "$USAGE_LINE" >&2; exit 2; }
eval set -- "$parsed"
while true; do
  case "$1" in
    -h|--help) usage; exit 0 ;;
    -n|--dry-run) DRY=1; shift ;;
    --) shift; break ;;
    *) echo "$USAGE_LINE" >&2; exit 2 ;;
  esac
done
if [[ $# -gt 0 ]]; then
  echo "$USAGE_LINE" >&2
  echo "publish-env-fix.sh: error: unexpected argument: $1" >&2
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

# The fixed loader must already be in this repo's env. Other checkouts are
# patched to that same block.
if ! grep -q 'Identity keys only' "$SRC/env"; then
  echo "ERROR: $SRC/env does not contain the fixed loader" >&2
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

# Print edit, already, missing, or diverged. On apply, edit rewrites the file.
patch_env() {
  local wt="$1"
  python3 - "$wt/env" "$DRY" <<'PY'
import os, stat, sys
path, dry = sys.argv[1], sys.argv[2] == "1"
old = (
    "  # shellcheck disable=SC1091\n"
    "  . <(sed 's/=/ /; s/^/export /' \"$SCRIPT_DIR/project.config\" "
    "| grep -E '^(code_group|shared_group|sandbox_group|primary_user|"
    "planning_user|coder_user|orchestrator_user)' || true)\n"
)
new = (
    "  # Identity keys only. Other project.config lines hold paths and must not be sourced.\n"
    "  while IFS= read -r _env_line || [ -n \"${_env_line:-}\" ]; do\n"
    "    case \"$_env_line\" in\n"
    "      code_group=*|shared_group=*|sandbox_group=*|primary_user=*|planning_user=*|coder_user=*|orchestrator_user=*)\n"
    "        _env_key=${_env_line%%=*}\n"
    "        _env_val=${_env_line#*=}\n"
    "        _env_val=${_env_val%$'\\r'}\n"
    "        export \"${_env_key}=${_env_val}\"\n"
    "        ;;\n"
    "    esac\n"
    "  done < \"$SCRIPT_DIR/project.config\"\n"
    "  unset _env_line _env_key _env_val\n"
)
if not os.path.isfile(path):
    print("missing")
    raise SystemExit(0)
with open(path, "r", encoding="utf-8", errors="replace") as f:
    text = f.read()
has_old = old in text
has_new = "Identity keys only" in text and 'export "${_env_key}=${_env_val}"' in text
if has_new and not has_old:
    print("already")
    raise SystemExit(0)
if text.count(old) == 1:
    print("edit")
    if dry:
        raise SystemExit(0)
    updated = text.replace(old, new, 1)
    st = os.stat(path)
    tmp = path + ".envfix.tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        f.write(updated)
        f.flush()
        os.fsync(f.fileno())
    os.chmod(tmp, stat.S_IMODE(st.st_mode))
    os.chown(tmp, st.st_uid, st.st_gid)
    os.replace(tmp, path)
    raise SystemExit(0)
if has_old:
    print("ambiguous")
else:
    print("diverged")
PY
}

commit_env() {
  local wt="$1"
  local -a paths=()
  local p one rest n line
  if [[ "$DRY" -eq 1 ]]; then
    echo "  would commit env"
    if [[ "$wt" == "$SRC" ]]; then
      echo "  would commit publish-env-fix.sh"
    fi
    return 0
  fi
  one="$(git_owner "$wt" status --porcelain -- env)" || {
    note_fail "git status failed for $wt/env"
    return 1
  }
  if [[ -n "$one" ]]; then
    paths+=(env)
  fi
  if [[ "$wt" == "$SRC" ]]; then
    one="$(git_owner "$wt" status --porcelain -- publish-env-fix.sh)" || {
      note_fail "git status failed for $wt/publish-env-fix.sh"
      return 1
    }
    if [[ -n "$one" ]]; then
      paths+=(publish-env-fix.sh)
    fi
  fi
  if [[ ${#paths[@]} -eq 0 ]]; then
    echo "  commit: env already matches HEAD"
    return 0
  fi
  echo "  commit: ${paths[*]}"
  clear_skip_worktree "$wt" "${paths[@]}" || return 1
  git_owner "$wt" add -f -- "${paths[@]}" || {
    note_fail "git add failed in $wt"
    return 1
  }
  git_owner "$wt" commit --only \
    -m "fix: load project.config identity keys in env" \
    -m "source ./env stopped with primary_user unset because sed rewrote primary_user=dlang into an export that grep then discarded. Assign the identity keys directly." \
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
echo "GIT_HOME: $GIT_HOME"
echo "dry-run: $DRY"
echo "Commits only env (and this script here). Does not push."

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
    st="$(patch_env "$wt")" || { note_fail "patch env in $wt"; continue; }
    echo "  $st env"
    case "$st" in
      edit|already) ;;
      missing)
        echo "  no env; left unchanged"
        continue
        ;;
      *)
        note_fail "$wt: env loader was $st; left unchanged"
        continue
        ;;
    esac
    if [[ "$FAILURES" -gt "$fails_before" ]]; then
      echo "  skip commit"
      continue
    fi
    commit_env "$wt" || true
  done
done

echo ""
if [[ "$FAILURES" -gt 0 ]]; then
  echo "Done with $FAILURES error(s). Not pushed."
  exit 1
fi
echo "Done. Not pushed."
