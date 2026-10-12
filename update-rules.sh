#!/usr/bin/env bash
# update-rules.sh: sync this host's rule files (rules.manifest + rules.manifest.local)
# from this orchestration root into its own worktrees. Thin wrapper around
# deploy-orchestration --host-only. Default is a report; pass --apply to commit.
# To publish across all projects use ./deploy-orchestration (from any host).
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
for a in "$@"; do
  case "$a" in
    --force|-f) echo "update-rules.sh: --force is not supported; dirty/diverged worktrees are skipped and reported." >&2; exit 2 ;;
  esac
done
exec "$here/deploy-orchestration" --host-only "$@"
