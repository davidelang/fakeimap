#!/usr/bin/env bash
# generate_pr.sh — Create a local Pull Request markdown for Master review.
# Usage: ./generate_pr.sh plan1.md [plan2.md ...]
#
# Writes: $SANDBOX/PRs/PR-<branch>.md
# SANDBOX from project.config sandbox_dir (or sandbox_path), else
# dev-ai-interaction/ if present, else sandbox/.
#
# Same script on VehicleExpenses and first-party library hosts.

set -euo pipefail

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
  echo "Usage: ./generate_pr.sh plan1.md [plan2.md ...]"
  exit 0
fi

if [[ $# -lt 1 ]]; then
  echo "Usage: $0 plan1.md [plan2.md ...]" >&2
  exit 1
fi

ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$ROOT"

if [[ -f project.config ]]; then
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line%%#*}"
    line="${line#"${line%%[![:space:]]*}"}"
    line="${line%"${line##*[![:space:]]}"}"
    [[ -z "$line" ]] && continue
    [[ "$line" =~ ^[a-zA-Z_][a-zA-Z0-9_]*= ]] || continue
    # shellcheck disable=SC2163
    export "$line"
  done <project.config
fi

SANDBOX_REL="${sandbox_dir:-${sandbox_path:-}}"
if [[ -z "$SANDBOX_REL" ]]; then
  if [[ -d "$ROOT/dev-ai-interaction" ]]; then
    SANDBOX_REL="dev-ai-interaction"
  elif [[ -d "$ROOT/sandbox" ]]; then
    SANDBOX_REL="sandbox"
  else
    SANDBOX_REL="sandbox"
  fi
fi
if [[ "$SANDBOX_REL" = /* ]]; then
  SANDBOX_DIR="$SANDBOX_REL"
else
  SANDBOX_DIR="$ROOT/$SANDBOX_REL"
fi

PLANS=("$@")
BRANCH_NAME=$(git rev-parse --abbrev-ref HEAD)
BACKUP_TAG="backup-$BRANCH_NAME"
PR_DIR="$SANDBOX_DIR/PRs"
PR_FILE="$PR_DIR/PR-$BRANCH_NAME.md"

if [[ "$BRANCH_NAME" == "master" || "$BRANCH_NAME" == "orchestration" ]]; then
  echo "Error: Cannot generate PR from branch '$BRANCH_NAME'." >&2
  exit 1
fi

if ! git rev-parse "$BACKUP_TAG" >/dev/null 2>&1; then
  echo "Warning: Backup tag '$BACKUP_TAG' not found. Ensure you have run cleanup/squash if required."
fi

mkdir -p "$PR_DIR"

{
  echo "# Pull Request: $BRANCH_NAME"
  echo ""
  echo "## Recovery & Audit Info"
  echo "- **Original Messy State:** \`$BACKUP_TAG\` ($(git rev-parse "$BACKUP_TAG" 2>/dev/null || echo "NOT FOUND"))"
  echo "- **Cleaned HEAD:** \`$(git rev-parse HEAD)\`"
  echo "- **Sandbox:** \`$SANDBOX_DIR\`"
  echo ""
  echo "## Logical Commit History"
  git log --pretty=format:"* %s (%h)" master..HEAD
  echo ""
  echo ""
  echo "## Documentation & Plans"
  for plan in "${PLANS[@]}"; do
    echo "### Plan: $plan"
    echo '```markdown'
    if [[ -f "$plan" ]]; then
      cat "$plan"
    elif [[ -f "$SANDBOX_DIR/$plan" ]]; then
      cat "$SANDBOX_DIR/$plan"
    elif [[ -f "$ROOT/$plan" ]]; then
      cat "$ROOT/$plan"
    else
      echo "(plan file not found: $plan)"
    fi
    echo '```'
    echo ""
  done
  echo "## Files Changed"
  echo '```'
  git diff --stat master..HEAD
  echo '```'
} >"$PR_FILE"

echo "PR Generated: $PR_FILE"
echo "--------------------------------------------------------------------------------"
echo "INSTRUCTION FOR USER:"
echo "Switch to the Master Agent terminal and say:"
echo "  \"Please review PR-$BRANCH_NAME\""
echo "--------------------------------------------------------------------------------"
