#!/bin/bash
# install-merge-drivers.sh — configure repo-local git config: merge drivers for special files
# and the manage-configs smudge/clean filter (required=true). The one place this config is set.
# Idempotent. Safe to run from any worktree (shared .git config).
# Called by update-rules.sh and setup_agent.sh; also runnable by hand.
# Project additions: if the worktree root has install-merge-drivers.local, it is run last with
# bash (from the root); a non-zero exit fails this script. That file is project-owned: it is not in
# rules.manifest, so deploy-orchestration never writes, overwrites or deletes it. Absent = skipped.

set -euo pipefail

USAGE_LINE="usage: ./orch/install-merge-drivers.sh [-h]"
usage() {
  cat <<EOF
$USAGE_LINE

Configure repo-local merge drivers and the manage-configs filter.

options:
  -h, --help  show this help message and exit

Details: the comment at the top of ./orch/install-merge-drivers.sh.
EOF
}
case "${1:-}" in
  -h|--help) usage; exit 0 ;;
  -[A-Za-z-]*) echo "$USAGE_LINE" >&2; echo "install-merge-drivers.sh: error: unrecognized option: $1" >&2; exit 2 ;;
esac

if ! ROOT=$(git rev-parse --show-toplevel); then
  echo "install-merge-drivers: ERROR: not inside a git worktree" >&2
  exit 1
fi
cd "$ROOT"

# autostash reset --hard cannot unlink chattr +a ENGINEERING_LOG.md
git config merge.autostash false

git config merge.ve-englog.name "VE eng-log append via wrapper (third-version result)"
git config merge.ve-englog.driver "./git-merge-drivers/ve-englog %O %A %B %L %P"

git config merge.ve-special-ours.name "VE special-file keep-ours (TODO, project-facts)"
git config merge.ve-special-ours.driver "./git-merge-drivers/ve-special-ours %O %A %B %L %P"

# manage-configs: @@TOKEN@@ <-> project.config values. required=true: a failing filter fails the git command.
git config filter.manage-configs.clean "bash ./filter-clean-config %f"
git config filter.manage-configs.smudge "bash ./filter-apply-config"
git config filter.manage-configs.required true

# Legacy alias: refuse now delegates to ve-special-ours
git config merge.ve-special-refuse.name "VE special-file keep-ours (deprecated alias)"
git config merge.ve-special-refuse.driver "./git-merge-drivers/ve-special-refuse %O %A %B %L %P"

chmod +x "$ROOT/git-merge-drivers/ve-englog" \
         "$ROOT/git-merge-drivers/ve-special-ours" \
         "$ROOT/git-merge-drivers/ve-special-refuse" \
         "$ROOT/orch/install-merge-drivers.sh" "$ROOT/orch/merge-branch-into-master.sh"

COMMON_DIR=$(git rev-parse --git-common-dir)
echo "install-merge-drivers: configured for $COMMON_DIR"
echo "  merge.autostash=$(git config --get merge.autostash)"
git config --get merge.ve-englog.driver
git config --get merge.ve-special-ours.driver
echo "  filter.manage-configs.required=$(git config --get filter.manage-configs.required)"

LOCAL="$ROOT/install-merge-drivers.local"
if [ -e "$LOCAL" ]; then
  echo "install-merge-drivers: running project-owned $LOCAL"
  if ! bash "$LOCAL"; then
    echo "install-merge-drivers: ERROR: $LOCAL failed" >&2
    exit 1
  fi
fi
