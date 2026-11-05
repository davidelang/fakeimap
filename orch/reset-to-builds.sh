#!/bin/bash
# Reset the tracked tree to this branch's builds tag, or to HEAD with --head.
# No other ref is accepted.
#
# ENGINEERING_LOG.md, TODO.md and project-facts.md are never touched: not
# written, not copied over, not restored, no attribute changes. The log keeps
# every entry, failures included (it is usually root-owned and append-only).
# HEAD and the index move to the target; every other path in the worktree is
# rewound to the target and untracked (non-ignored) files are removed. After
# the reset the protected files show as unstaged changes if they differ from
# the target. No scratch files are used.
set -euo pipefail

MODE=builds
parsed="$(getopt -o hH -l help,head -- "$@")" || exit 2
eval set -- "$parsed"
while true; do
  case "$1" in
    -h|--help) echo "usage: ./orch/reset-to-builds.sh [-H|--head]"; exit 0 ;;
    -H|--head) MODE=head; shift ;;
    --) shift; break ;;
    *) echo "usage: ./orch/reset-to-builds.sh [--head]" >&2; exit 2 ;;
  esac
done
if [ $# -gt 0 ]; then
  echo "usage: ./orch/reset-to-builds.sh [--head]" >&2
  exit 2
fi

root=$(git rev-parse --show-toplevel)
cd "$root"

if [ "$MODE" = head ]; then
  target=$(git rev-parse --verify HEAD^{commit})
else
  tag=$(./orch/get-builds-tag.sh)
  target=$(git rev-parse --verify "$tag^{commit}")
fi

# Root names stay excluded so a symlink is not replaced. The work-history
# blobs are the append-only log and the TODO content after the move.
keep=(
  ':(exclude)ENGINEERING_LOG.md'
  ':(exclude)TODO.md'
  ':(exclude)project-facts.md'
  ':(exclude)work-history/ENGINEERING_LOG.md'
  ':(exclude)work-history/TODO.md'
)

# HEAD and index to target; worktree untouched by this step.
git reset --quiet --mixed "$target"
# Worktree of every tracked path except the protected files to target.
git restore --source="$target" --worktree -- . "${keep[@]}"
# Untracked, non-ignored files (including paths only in the old HEAD).
git clean -fd --quiet -- . "${keep[@]}"

echo "reset-to-builds: $MODE done at ${target:0:12}. ENGINEERING_LOG.md, TODO.md, project-facts.md untouched (unstaged if they differ)."
git status --short
