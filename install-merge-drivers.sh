#!/bin/bash
# install-merge-drivers.sh — configure repo-local git config: merge drivers for special files
# and the manage-configs smudge/clean filter (required=true). The one place this config is set.
# Idempotent. Safe to run from any worktree (shared .git config).
# Called by update-rules.sh and setup_agent.sh; also runnable by hand.

set -euo pipefail

ROOT=$(git rev-parse --show-toplevel 2>/dev/null || true)
if [ -z "$ROOT" ]; then
  echo "install-merge-drivers: ERROR: not inside a git worktree" >&2
  exit 1
fi

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
         "$ROOT/git-merge-drivers/ve-special-refuse" 2>/dev/null || true
chmod +x "$ROOT/install-merge-drivers.sh" "$ROOT/merge-branch-into-master.sh" 2>/dev/null || true

echo "install-merge-drivers: configured for $(git rev-parse --git-common-dir 2>/dev/null || echo .git)"
echo "  merge.autostash=$(git config --get merge.autostash)"
git config --get merge.ve-englog.driver
git config --get merge.ve-special-ours.driver
echo "  filter.manage-configs.required=$(git config --get filter.manage-configs.required)"