#!/bin/bash
# publish-release-tag.sh — commit optional --release-tag and publish that build_app.
#
# Commits, on this master, the current android-gradle template, its README,
# tools/test-build-play, and this script. Does not push.
#
# Then publishes that build_app onto:
#   - every other master worktree whose build_app is an older published copy
#     of build-examples/android-gradle/build_app
#   - the project root of a host whose committed project-facts.md says
#     `- **Build type:** android-gradle`, when that root's build_app is an
#     older published copy (that checkout is often branch orchestration;
#     it is the copy later syncs carry into worktrees)
# A copy that was never the template is left alone. Other branches (agent
# worktrees, feature branches) are left alone.
#
# The example README (build-examples/android-gradle/README.md) is updated
# on this master, and on a master that already has that file and uses
# build_app. It is not created anywhere. deploy-orchestration does not
# install it. This script does not run deploy-orchestration.
#
# Owner and mode of every replaced file are kept.
#
#   sudo ./publish-release-tag.sh --dry-run
#   sudo ./publish-release-tag.sh
set -euo pipefail

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DRY=0
USAGE_LINE="usage: ./publish-release-tag.sh [-h] [-n]"
usage() {
  cat <<EOF
$USAGE_LINE

Commit the optional --release-tag build_app change on this master (root only).

options:
  -h, --help     show this help message and exit
  -n, --dry-run  print what would change and change nothing

Details: the comment at the top of ./publish-release-tag.sh.
EOF
}
parsed="$(getopt -n publish-release-tag.sh -o hn -l help,dry-run -- "$@")" || { echo "$USAGE_LINE" >&2; exit 2; }
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
  echo "publish-release-tag.sh: error: unexpected argument: $1" >&2
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

TEMPLATE="$SRC/build-examples/android-gradle/build_app"
README_REL="build-examples/android-gradle/README.md"
if [[ ! -f "$TEMPLATE" ]]; then
  echo "ERROR: $TEMPLATE is missing" >&2
  exit 1
fi
if ! grep -q -- '--release-tag NAME' "$TEMPLATE" || ! grep -q 'create_release_tag' "$TEMPLATE"; then
  echo "ERROR: $TEMPLATE does not contain --release-tag" >&2
  exit 1
fi
bash -n "$TEMPLATE"

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

git_as() {
  local wt="$1" owner="$2"
  shift 2
  if [[ "$(id -u)" -eq 0 ]]; then
    sudo -u "$owner" git -c "safe.directory=$wt" -c "safe.directory=*" -C "$wt" "$@"
  else
    git -c "safe.directory=$wt" -c "safe.directory=*" -C "$wt" "$@"
  fi
}

clear_skip_worktree() {
  local wt="$1" owner="$2"
  shift 2
  local p tracked
  for p in "$@"; do
    tracked="$(git_as "$wt" "$owner" ls-files -- "$p")" || {
      note_fail "git ls-files failed for $p in $wt"
      return 1
    }
    if [[ -n "$tracked" ]]; then
      git_as "$wt" "$owner" update-index --no-skip-worktree -- "$p" || {
        note_fail "could not clear skip-worktree on $wt/$p"
        return 1
      }
    fi
  done
}

build_type() {
  local wt="$1" out
  if ! git -c "safe.directory=*" -C "$wt" rev-parse -q --verify HEAD:project-facts.md >/dev/null; then
    return 0
  fi
  out="$(git -c "safe.directory=*" -C "$wt" show HEAD:project-facts.md)"
  printf '%s\n' "$out" | python3 -c '
import sys
lines = sys.stdin.read().splitlines()
heads = [i for i, l in enumerate(lines) if l.rstrip() == "## Build and commit"]
if len(heads) != 1:
    raise SystemExit(0)
start = heads[0]
end = next((i for i in range(start + 1, len(lines)) if lines[i].startswith("#")), len(lines))
prefix = "- **Build type:** "
vals = []
for i, l in enumerate(lines):
    if start < i < end and l.startswith(prefix):
        vals.append(l[len(prefix):].strip())
if len(vals) == 1:
    print(vals[0])
'
}

# Status word on stdout: edit, already, missing, mismatch, dirty, error.
patch_readme() {
  local path="$1"
  python3 - "$path" "$DRY" <<'PY'
import os, stat, sys
path, dry = sys.argv[1], sys.argv[2] == "1"
old_usage = './build_app [--standard] [--test] [--play|--bundle] ["msg" | @msg_file] [files...] [-- gradle-flags]'
new_usage = './build_app [--standard] [--test] [--play|--bundle] [--release-tag NAME] ["msg" | @msg_file] [files...] [-- gradle-flags]'
old_flags = "- Flags come first, in any order, and can be combined.\n"
new_flags = "- Flags come first, in any order, and can be combined. `--release-tag NAME` may be among them; it does not select a build.\n"
old_steps = """5. **versionCode bump (Play only, before any build):** right after the checks above,
   `app/versionCode.txt` goes from N to N+1 and is committed alone as
   `versionCode N+1 (play build)`, by `coder_user` with the invoker's git identity.
   - A missing file or a non-integer value stops the run loudly. Without `--play` nothing
     is bumped. No git tag is created; tag the bump commit yourself if you want one.
   - If a later build fails, the bump commit stays (history is never rewritten or undone);
     the next `--play` bumps again. Gaps in versionCode are fine for Play. The run says so.
6. **Builds:** they run in sequence (standard, test, play), all from that one HEAD (the bump
   commit when `--play` is selected), so every record shares one commit and is current for
   deploy. The first failure stops everything.
   - versionName is the `<branch>-start` describe when that tag exists, otherwise
     `git describe --always --exclude '*+*'` of that commit.
7. **Tag:** the builds tag (`builds` / `<branch>/builds`) moves to the built commit only after
   every selected build succeeded.

For a release: commit your work (with build_app or git), then `./build_app --play`. No
manual version number is needed. build_app makes no release tag; tag versions yourself
(`git tag -a v1.0.0`) when you want them.
"""
new_steps = """5. **versionCode bump (Play only, before any build):** right after the checks above,
   `app/versionCode.txt` goes from N to N+1 and is committed alone as
   `versionCode N+1 (play build)`, by `coder_user` with the invoker's git identity.
   - A missing file or a non-integer value stops the run loudly. Without `--play` nothing
     is bumped.
   - If a later build fails, the bump commit stays (history is never rewritten or undone);
     the next `--play` bumps again. Gaps in versionCode are fine for Play. The run says so.
6. **Release tag (`--release-tag NAME` only, before any build):** an annotated tag of that
   name is created on the commit these builds will use. With `--play` that is the bump
   commit. Without `--play` nothing is bumped, and the tag is on the commit the selected
   builds use. There is no prompt. Omitting the flag creates no release tag.
   - Refused before any bump or tag when the name is missing, the tag already exists,
     the name is `builds` or ends in `/builds`, the name ends in `-start`, the name
     contains `+` (`git describe` excludes those, so it would not be the versionName),
     the checkout is detached, or this branch has a `<branch>-start` tag. That testing
     describe would hide the release name. Release tags are for a branch without one,
     such as master.
   - A name is used once. Tagging a commit `v1.0.0` uses that name up. A later `--play`
     is a new commit and describes as `v1.0.0-1-g<hash>` unless that run passes its own
     new tag. Pass `--release-tag` on the Play run when the upload must read `v1.0.0`.
   - If a build fails, the tag this run created is deleted. A versionCode bump commit
     stays.
7. **Builds:** they run in sequence (standard, test, play), all from that one HEAD (the bump
   commit when `--play` is selected), so every record shares one commit and is current for
   deploy. The first failure stops everything.
   - versionName is the `<branch>-start` describe when that tag exists, otherwise
     `git describe --always --exclude '*+*'` of that commit. An exact `--release-tag` on
     that commit is the whole versionName.
8. **Tag:** the builds tag (`builds` / `<branch>/builds`) moves to the built commit only after
   every selected build succeeded.

For a release whose upload must read `v1.0.0`:

```
./build_app --play --release-tag v1.0.0
```

The annotated tag is created after the versionCode bump and before Gradle, so standard,
test, and Play all describe as `v1.0.0`. A run with no `--release-tag` creates no release
tag and does not ask for one. The same flag tags without Play, and does not bump
versionCode:

```
./build_app --standard --test --release-tag v1.0.0
```
"""
if not os.path.isfile(path):
    print("missing")
    raise SystemExit(0)
with open(path, encoding="utf-8", newline="") as f:
    text = f.read()
pairs = ((old_usage, new_usage), (old_flags, new_flags), (old_steps, new_steps))
if all(new in text and old not in text for old, new in pairs):
    print("already")
    raise SystemExit(0)
if not all(text.count(old) == 1 and new not in text for old, new in pairs):
    print("mismatch")
    raise SystemExit(0)
print("edit")
if dry:
    raise SystemExit(0)
updated = text
for old, new in pairs:
    updated = updated.replace(old, new, 1)
st = os.stat(path)
tmp = path + ".releasetag.tmp"
with open(tmp, "w", encoding="utf-8", newline="") as f:
    f.write(updated)
    f.flush()
    os.fsync(f.fileno())
os.chmod(tmp, stat.S_IMODE(st.st_mode))
os.chown(tmp, st.st_uid, st.st_gid)
os.replace(tmp, path)
PY
}

SUBJECT='build_app: optional --release-tag on the commit Gradle builds'
BODY='--release-tag NAME creates an annotated tag before Gradle. With --play the tag is on the versionCode bump; without --play it is on the commit the selected builds use. There is no prompt. Omitting the flag creates no release tag. A failed build deletes the tag this run created. A versionCode bump commit stays.

The file is the current build-examples/android-gradle/build_app from orchestration-example.'
ORCH_BODY="$BODY"$'\n\nDocuments the flag in build-examples/android-gradle/README.md. tools/test-build-play covers it.'

# During --dry-run the README and build_app are not written, so a path listed
# in FORCE_COMMIT is reported as a commit even while the worktree is still clean.
FORCE_COMMIT=()

commit_paths() {
  local wt="$1" owner="$2" body="$3"
  shift 3
  local -a paths=("$@")
  local -a changed=()
  local p one f forced
  if [[ "$DRY" -eq 1 ]]; then
    for p in "${paths[@]}"; do
      one="$(git_as "$wt" "$owner" status --porcelain -- "$p" || true)"
      forced=0
      for f in "${FORCE_COMMIT[@]}"; do
        [[ "$f" == "$p" ]] && forced=1
      done
      if [[ -n "$one" || "$forced" -eq 1 ]]; then
        changed+=("$p")
      fi
    done
    if [[ ${#changed[@]} -eq 0 ]]; then
      echo "  commit: nothing to commit"
    else
      echo "  would commit: ${changed[*]}"
    fi
    return 0
  fi
  for p in "${paths[@]}"; do
    one="$(git_as "$wt" "$owner" status --porcelain -- "$p")" || {
      note_fail "git status failed for $wt/$p"
      return 1
    }
    if [[ -n "$one" ]]; then
      changed+=("$p")
    fi
  done
  if [[ ${#changed[@]} -eq 0 ]]; then
    echo "  commit: already matches HEAD"
    return 0
  fi
  echo "  commit: ${changed[*]}"
  clear_skip_worktree "$wt" "$owner" "${changed[@]}" || return 1
  git_as "$wt" "$owner" add -f -- "${changed[@]}" || {
    note_fail "git add failed in $wt"
    return 1
  }
  git_as "$wt" "$owner" commit --only -m "$SUBJECT" -m "$body" -- "${changed[@]}" || {
    note_fail "git commit failed in $wt"
    return 1
  }
  echo "  committed $(git_as "$wt" "$owner" rev-parse --short HEAD) on $(git_as "$wt" "$owner" branch --show-current)"
}

CANON_BLOBS="$(
  git -c "safe.directory=*" -C "$SRC" log --format= --raw --no-abbrev -- build-examples/android-gradle/build_app \
    | awk '/^:/ {print $3; print $4}'
  git hash-object -- "$TEMPLATE"
)"
CANON_BLOBS="$(printf '%s\n' "$CANON_BLOBS" | grep -v '^0\{40\}$' | sort -u)"

BR="$(git -c "safe.directory=*" -C "$SRC" branch --show-current)"
if [[ "$BR" != master ]]; then
  echo "ERROR: $SRC is on ${BR:-detached}, not master" >&2
  exit 1
fi

OWNER="$(cfg_get "$SRC/project.config" primary_user)"
if [[ -z "$OWNER" ]]; then
  OWNER="$(stat -c '%U' "$SRC")"
fi
if [[ "$DRY" -eq 0 ]] && ! id "$OWNER" >/dev/null 2>&1; then
  echo "ERROR: user $OWNER does not exist" >&2
  exit 1
fi

echo "Source: $SRC (master)"
echo "Git owner here: $OWNER"
echo "dry-run: $DRY"
echo "Commits the template, the example README, and the tests. Does not push."
echo "Publishes build_app to other masters that still have a published copy, and to android-gradle project roots."
echo "Leaves a build_app that was never the template unchanged. Does not touch other branches."

echo ""
echo "======== $SRC"
rst="$(patch_readme "$SRC/$README_REL")" || { note_fail "README patch failed in $SRC"; exit 1; }
echo "  $rst $README_REL"
if [[ "$rst" == mismatch || "$rst" == error ]]; then
  note_fail "README in $SRC does not match the expected release section; nothing committed"
  exit 1
fi
if [[ "$rst" == dirty ]]; then
  note_fail "README in $SRC has uncommitted changes; nothing committed"
  exit 1
fi
FORCE_COMMIT=()
if [[ "$rst" == edit ]]; then
  FORCE_COMMIT+=("$README_REL")
fi
commit_paths "$SRC" "$OWNER" "$ORCH_BODY" \
  build-examples/android-gradle/build_app \
  "$README_REL" \
  tools/test-build-play \
  publish-release-tag.sh || exit 1

declare -A SEEN=()
SEEN["$(realpath "$SRC")"]=1

shopt -s nullglob
for d in "$GIT_HOME"/*/; do
  root="${d%/}"
  base="$(basename "$root")"
  if [[ "$base" == *.git-backup || "$base" == *git-history* ]]; then
    continue
  fi
  if [[ ! -f "$root/build_app" && ! -f "$root/$README_REL" && ! -f "$root/project-facts.md" && ! -f "$root/setup_agent.sh" ]]; then
    continue
  fi
  if ! git -c "safe.directory=*" -C "$root" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    continue
  fi
  owner="$(cfg_get "$root/project.config" primary_user)"
  if [[ -z "$owner" ]]; then
    owner="$(stat -c '%U' "$root")"
  fi
  if [[ "$DRY" -eq 0 ]] && ! id "$owner" >/dev/null 2>&1; then
    note_fail "$root: user $owner does not exist"
    continue
  fi
  mapfile -t WTS < <(git -c "safe.directory=*" -C "$root" worktree list --porcelain | awk '/^worktree / {print substr($0, 10)}')
  for wt in "${WTS[@]}"; do
    if [[ ! -d "$wt" ]]; then
      note_fail "worktree path missing: $wt"
      continue
    fi
    rp="$(realpath "$wt")" || {
      note_fail "realpath failed for $wt"
      continue
    }
    [[ -z "${SEEN[$rp]:-}" ]] || continue
    SEEN["$rp"]=1
    [[ -f "$wt/build_app" || -f "$wt/$README_REL" ]] || continue
    if ! br="$(git -c "safe.directory=*" -C "$wt" branch --show-current)"; then
      note_fail "could not read the branch of $wt"
      continue
    fi
    echo ""
    echo "======== $wt"
    echo "  branch: ${br:-detached}  git owner: $owner"
    if [[ -z "$br" ]]; then
      echo "  detached; left unchanged"
      continue
    fi
    is_root=0
    [[ "$rp" == "$(realpath "$root")" ]] && is_root=1
    bt="$(build_type "$wt")"
    publish_app=0
    if [[ -f "$wt/build_app" ]]; then
      if [[ "$br" == master || ( "$is_root" -eq 1 && "$bt" == android-gradle ) ]]; then
        publish_app=1
      fi
    fi
    if [[ -f "$wt/build_app" && "$publish_app" -eq 0 ]]; then
      echo "  build_app left unchanged (branch $br)"
    fi
    if [[ "$publish_app" -eq 1 ]]; then
      por="$(git_as "$wt" "$owner" status --porcelain -- build_app || true)"
      blob="$(git hash-object -- "$wt/build_app")"
      if [[ -n "$por" ]]; then
        echo "  build_app has uncommitted changes ($blob); left unchanged"
        note_fail "$wt/build_app is dirty"
      else
        # install_build_app expects the blob id as argv 4. The heredoc is unquoted
        # so CANON_BLOBS expands; argv stays positional from python3's arguments.
        st="$(python3 - "$wt/build_app" "$TEMPLATE" "$DRY" "$blob" <<PY
import os, stat, sys
dest, src, dry, blob = sys.argv[1], sys.argv[2], sys.argv[3] == "1", sys.argv[4]
blobs = set("""$CANON_BLOBS""".split())
if not os.path.isfile(dest):
    print("missing")
    raise SystemExit(0)
data = open(dest, "rb").read()
target = open(src, "rb").read()
if data == target:
    print("already")
    raise SystemExit(0)
if blob not in blobs:
    print("diverged")
    raise SystemExit(0)
print("edit")
if dry:
    raise SystemExit(0)
stt = os.stat(dest)
tmp = dest + ".releasetag.tmp"
with open(tmp, "wb") as f:
    f.write(target)
    f.flush()
    os.fsync(f.fileno())
os.chmod(tmp, stat.S_IMODE(stt.st_mode))
os.chown(tmp, stt.st_uid, stt.st_gid)
os.replace(tmp, dest)
PY
)" || { note_fail "install build_app failed in $wt"; continue; }
        why="master"
        if [[ "$br" != master ]]; then
          why="project root, build type android-gradle"
        fi
        echo "  $st build_app ($why, blob ${blob:0:12})"
        if [[ "$st" == diverged ]]; then
          if grep -q 'fuelsmoothing_official' "$wt/build_app"; then
            echo "  left unchanged: local fuelsmoothing_official; this file was never the published template"
          else
            echo "  left unchanged: this file was never the published template"
          fi
        elif [[ "$st" == edit || "$st" == already ]]; then
          FORCE_COMMIT=()
          if [[ "$st" == edit ]]; then
            FORCE_COMMIT+=(build_app)
          fi
          commit_paths "$wt" "$owner" "$BODY" build_app || true
        fi
      fi
    fi
    if [[ -f "$wt/$README_REL" ]]; then
      if [[ "$br" != master ]]; then
        echo "  $README_REL left unchanged (not master)"
      elif [[ ! -f "$wt/build_app" ]]; then
        echo "  $README_REL still has the old release text; left unchanged (this master has no build_app)"
      else
        rpor="$(git_as "$wt" "$owner" status --porcelain -- "$README_REL" || true)"
        if [[ -n "$rpor" ]]; then
          echo "  $README_REL has uncommitted changes; left unchanged"
          note_fail "$wt/$README_REL is dirty"
        else
          rst="$(patch_readme "$wt/$README_REL")" || { note_fail "README patch failed in $wt"; continue; }
          echo "  $rst $README_REL"
          if [[ "$rst" == mismatch ]]; then
            note_fail "$wt/$README_REL release section does not match the template; left unchanged"
          elif [[ "$rst" == edit || "$rst" == already ]]; then
            FORCE_COMMIT=()
            if [[ "$rst" == edit ]]; then
              FORCE_COMMIT+=("$README_REL")
            fi
            commit_paths "$wt" "$owner" "$BODY" "$README_REL" || true
          fi
        fi
      fi
    elif [[ "$br" == master && -f "$wt/build_app" ]]; then
      echo "  no $README_REL (deploy does not install it)"
    fi
  done
done
shopt -u nullglob

echo ""
if [[ "$FAILURES" -gt 0 ]]; then
  echo "Done with $FAILURES error(s). Not pushed."
  exit 1
fi
echo "Done. Not pushed."
