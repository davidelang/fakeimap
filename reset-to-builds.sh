#!/bin/bash
# Reset the tracked tree to this branch's builds tag, or to HEAD with --head.
# ENGINEERING_LOG.md, TODO.md, and project-facts.md are put back afterward.
# No other ref is accepted.
set -euo pipefail

MODE=builds
if [ "${1:-}" = "--head" ]; then
  MODE=head
elif [ -n "${1:-}" ]; then
  echo "usage: ./reset-to-builds.sh [--head]" >&2
  exit 2
fi

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "ERROR: reset-to-builds.sh must be run from inside a git worktree." >&2
  exit 1
fi

root=$(git rev-parse --show-toplevel)
cd "$root"

specials=(ENGINEERING_LOG.md TODO.md project-facts.md)
saved=$(mktemp -d)
trap 'rm -rf "$saved"' EXIT

for f in "${specials[@]}"; do
  if [ -f "$f" ]; then
    mkdir -p "$saved/$(dirname "$f")"
    cp -a "$f" "$saved/$f"
  fi
done

log_attr=
if [ -f ENGINEERING_LOG.md ] && command -v lsattr >/dev/null 2>&1; then
  log_attr=$(lsattr -a ENGINEERING_LOG.md 2>/dev/null | awk '{print $1}' || true)
fi
cleared_append=0
case "$log_attr" in
  *a*)
    if chattr -a ENGINEERING_LOG.md 2>/dev/null; then
      cleared_append=1
    else
      echo "ERROR: ENGINEERING_LOG.md is append-only and chattr -a failed. No reset was done." >&2
      exit 1
    fi
    ;;
esac

restore_append() {
  if [ "$cleared_append" -eq 1 ] && [ -f ENGINEERING_LOG.md ]; then
    chattr +a ENGINEERING_LOG.md 2>/dev/null || \
      echo "WARN: could not restore append-only on ENGINEERING_LOG.md" >&2
  fi
}
trap 'restore_append; rm -rf "$saved"' EXIT

git update-index --skip-worktree ENGINEERING_LOG.md 2>/dev/null || true

if [ "$MODE" = head ]; then
  git reset --hard HEAD
else
  tag=$(./get-builds-tag.sh)
  git reset --hard "$tag"
fi

git update-index --no-skip-worktree ENGINEERING_LOG.md 2>/dev/null || true

for f in "${specials[@]}"; do
  if [ -f "$saved/$f" ]; then
    cp -a "$saved/$f" "$f"
  fi
done

restore_append
cleared_append=0
echo "reset-to-builds: $MODE done. Special files restored, left unstaged."
