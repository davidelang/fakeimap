#!/bin/bash
#
# get-builds-tag.sh
#
# Safe, blessed helper for agents (Grok, Gemini, etc.) to obtain the current
# per-branch builds tag for preflight verification before any git reset --hard.
#
# This encapsulates the exact logic from AGENT_MANDATES.md so agents do not
# have to inline a long, variable-containing command that triggers repeated
# permission prompts.
#
# Usage:
#   TAG=$(./orch/get-builds-tag.sh)
#   echo "Using tag: $TAG"
#   git rev-parse "$TAG"   # (helper already verified it exists)
#
# On success: prints the tag name (e.g. "feature-foo/builds" or "builds") to stdout.
# On failure (no such tag): prints error to stderr and exits 1.
#
# This script has no side effects and only performs read-only git operations.
# It is intended to be pre-approved in .grok/config.toml under the blessed
# scripts section so repeated invocations do not require per-use permission.
#
# Must be run from inside a git worktree (enforced).

set -euo pipefail

USAGE_LINE="usage: ./orch/get-builds-tag.sh [-h]"
usage() {
  cat <<EOF
$USAGE_LINE

Print this branch's builds tag (feature-foo/builds, or builds on master).

options:
  -h, --help  show this help message and exit

Details: the comment at the top of ./orch/get-builds-tag.sh.
EOF
}
case "${1:-}" in
  -h|--help) usage; exit 0 ;;
  -[A-Za-z-]*) echo "$USAGE_LINE" >&2; echo "get-builds-tag.sh: error: unrecognized option: $1" >&2; exit 2 ;;
esac

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    echo "ERROR: get-builds-tag.sh must be run from inside a git worktree." >&2
    exit 1
fi

BRANCH=$(git rev-parse --abbrev-ref HEAD)

if [ "$BRANCH" = "master" ]; then
    TAG="builds"
else
    TAG="${BRANCH}/builds"
fi

if git rev-parse --verify "$TAG" >/dev/null 2>&1; then
    echo "$TAG"
    exit 0
else
    echo "ERROR: Required builds tag '$TAG' does not exist on branch '$BRANCH'." >&2
    echo "Run a successful ./build_app --standard --test first (from inside the appropriate worktree) to create it." >&2
    exit 1
fi
