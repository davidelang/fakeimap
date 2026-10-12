#!/bin/bash
# remove_worktree.sh: Safely remove agent worktrees and their branches
# Usage: ./remove_worktree.sh [-f|-ff] agent-N|branch-name

FORCE_LEVEL=0
TARGET=""

while [[ $# -gt 0 ]]; do
    case $1 in
        -ff)
            FORCE_LEVEL=2
            shift
            ;;
        -f|--force)
            FORCE_LEVEL=$((FORCE_LEVEL + 1))
            shift
            ;;
        *)
            TARGET="$1"
            shift
            ;;
    esac
done

if [ -z "$TARGET" ]; then
    echo "Usage: $0 [-f|-ff] agent-N|branch-name"
    exit 1
fi

if [ ! -r .git/config ]; then
    echo "Error: cannot read .git/config ($(stat -c '%A %U:%G' .git/config 2>/dev/null || echo missing))." >&2
    echo "  Run: source ./env" >&2
    echo "  Then re-run this command." >&2
    exit 1
fi

# 1. Resolve TARGET to the worktree directory and branch name
if [ -d "$TARGET" ] && [ ! -L "$TARGET" ]; then
    # TARGET is the directory (agent-N)
    WORKTREE_DIR="$TARGET"
    BRANCH_NAME=$(git -C "$WORKTREE_DIR" rev-parse --abbrev-ref HEAD 2>/dev/null)
elif [ -L "$TARGET" ]; then
    # TARGET is a symlink (branch-name.wt or legacy branch-name)
    WORKTREE_DIR=$(readlink "$TARGET")
    BRANCH_NAME="${TARGET%.wt}"
else
    # TARGET might be a branch name without a symlink
    BRANCH_NAME="$TARGET"
    WORKTREE_DIR=$(git worktree list --porcelain | grep -B 2 "branch refs/heads/$BRANCH_NAME" | grep "worktree" | awk '{print $2}')
    if [ -z "$WORKTREE_DIR" ]; then
        # Folder name of a worktree whose directory is already gone.
        WORKTREE_DIR=$(git worktree list --porcelain | awk -v t="$TARGET" '
            $1 == "worktree" {
                n = $2
                sub(/.*\//, "", n)
                if (n == t) print $2
            }
        ')
        if [ -n "$WORKTREE_DIR" ]; then
            BRANCH_NAME=$(git worktree list --porcelain | awk -v p="$WORKTREE_DIR" '
                $1 == "worktree" { cur = $2 }
                $1 == "branch" && cur == p {
                    sub(/^refs\/heads\//, "", $2)
                    print $2
                }
            ')
        fi
    fi
fi

if [ -z "$BRANCH_NAME" ] || [ -z "$WORKTREE_DIR" ]; then
    echo "Error: Could not resolve branch or worktree for '$TARGET'."
    exit 1
fi

# Convert WORKTREE_DIR to relative path if it's absolute and inside current dir
CURRENT_DIR=$(pwd)
WORKTREE_DIR=${WORKTREE_DIR#$CURRENT_DIR/}

DIR_PRESENT=1
if [ ! -d "$WORKTREE_DIR" ]; then
    DIR_PRESENT=0
    echo "Worktree directory '$WORKTREE_DIR' is already gone (Branch: $BRANCH_NAME)."
else
    echo "Targeting worktree: $WORKTREE_DIR (Branch: $BRANCH_NAME)"
fi

# 2. Status Checks
IS_MERGED=false
if git merge-base --is-ancestor "$BRANCH_NAME" refs/heads/master 2>/dev/null; then
    IS_MERGED=true
fi

HAS_UNIQUE_COMMITS=true
BRANCH_TIP=$(git rev-parse "$BRANCH_NAME" 2>/dev/null)
MERGE_BASE=$(git merge-base "$BRANCH_NAME" refs/heads/master 2>/dev/null)
if [ "$BRANCH_TIP" == "$MERGE_BASE" ]; then
    HAS_UNIQUE_COMMITS=false
fi

# 3. Safety Checks (only when the directory is still here to remove)
if [ "$DIR_PRESENT" -eq 1 ] && [ $FORCE_LEVEL -lt 1 ]; then
    # Check for uncommitted changes
    MODIFIED=$(git -C "$WORKTREE_DIR" status --porcelain -uno)
    if [ -n "$MODIFIED" ]; then
        echo "Error: Worktree '$WORKTREE_DIR' has uncommitted changes. Use -f to override."
        echo "$MODIFIED"
        exit 1
    fi

    # Check if branch is merged
    if [ "$IS_MERGED" = false ] && [ "$HAS_UNIQUE_COMMITS" = true ]; then
        echo "Warning: Branch '$BRANCH_NAME' has unique commits and has not been merged into master."
        echo "Use -f to remove the worktree, or -ff to also delete the branch."
        exit 1
    fi
fi

# 4. Removal of Worktree
if [ "$DIR_PRESENT" -eq 0 ]; then
    echo "Directory already gone; clearing the git worktree registration only."
else
# Reset permissions to allow deletion (worktrees may have root-owned files like logs,
# tight setgid, or chattr that prevent rm by non-root). This is needed because
# git worktree remove does an internal rm that respects current perms.
echo "Resetting permissions on '$WORKTREE_DIR' for deletion..."
chmod -R u+rwX "$WORKTREE_DIR" 2>/dev/null || true
# If root-owned files (e.g. ENGINEERING_LOG.md, append wrapper), escalate
if [ "$(stat -c %U "$WORKTREE_DIR/ENGINEERING_LOG.md" 2>/dev/null)" = "root" ] || \
   [ "$(stat -c %U "$WORKTREE_DIR/append-to-engineering-log" 2>/dev/null)" = "root" ]; then
    echo "  Root-owned files detected; using sudo for full cleanup..."
    sudo chmod -R u+rwX "$WORKTREE_DIR" 2>/dev/null || true
    sudo chown -R "$(id -un):$(id -gn)" "$WORKTREE_DIR" 2>/dev/null || true
fi
# Also handle possible chattr on log
sudo chattr -a "$WORKTREE_DIR/ENGINEERING_LOG.md" 2>/dev/null || true

# third_party: detach worktrees / empty src so rm -rf of agent dir succeeds
if [ -d "$WORKTREE_DIR/third_party" ]; then
  echo "Cleaning third_party/*/src under $WORKTREE_DIR ..."
  for _src in "$WORKTREE_DIR"/third_party/*/src; do
    [ -e "$_src" ] || continue
    chmod -R u+w "$_src" 2>/dev/null || true
    # If this is a linked worktree of a library host, remove via that repo
    if [ -f "$_src/.git" ]; then
      _gitdir=$(sed -n 's/^gitdir: //p' "$_src/.git" 2>/dev/null || true)
      if [ -n "$_gitdir" ] && [ -d "$_gitdir" ]; then
        _common=$(cd "$_gitdir/../.." 2>/dev/null && pwd)
        if [ -n "$_common" ] && [ -d "$_common/.git" ] || [ -f "$_common/.git" ]; then
          git -C "$_common" worktree remove --force "$_src" 2>/dev/null || true
        fi
      fi
    fi
    rm -rf "$_src" 2>/dev/null || sudo rm -rf "$_src" 2>/dev/null || true
    mkdir -p "$_src" 2>/dev/null || true
  done
fi

echo "Removing worktree '$WORKTREE_DIR'..."
git worktree remove --force "$WORKTREE_DIR" || {
    echo "git worktree remove failed (likely permissions). Re-applying permissive reset..."
    chmod -R u+rwX "$WORKTREE_DIR" 2>/dev/null || true
    sudo chmod -R u+rwX "$WORKTREE_DIR" 2>/dev/null || true
    sudo chown -R "$(id -un):$(id -gn)" "$WORKTREE_DIR" 2>/dev/null || true
    sudo chattr -a "$WORKTREE_DIR/ENGINEERING_LOG.md" 2>/dev/null || true
    git worktree remove --force "$WORKTREE_DIR" || {
        echo "Still failed. Falling back to direct rm -rf..."
        rm -rf "$WORKTREE_DIR" || sudo rm -rf "$WORKTREE_DIR"
    }
}

if [ -d "$WORKTREE_DIR" ]; then
    echo "Error: Failed to remove worktree directory."
    exit 1
fi
fi

# rm -rf fallback deletes the folder and leaves .git/worktrees/<id> behind.
# A later run used to stop at "directory not found" and never prune.
if ! git worktree prune -v; then
    echo "Error: git worktree prune failed. Registration for '$WORKTREE_DIR' may remain." >&2
    exit 1
fi

# 5. Cleanup Symlinks
if [ -L "${BRANCH_NAME}.wt" ]; then
    echo "Removing symlink '${BRANCH_NAME}.wt'..."
    rm "${BRANCH_NAME}.wt"
elif [ -L "$BRANCH_NAME" ]; then
    echo "Removing symlink '$BRANCH_NAME'..."
    rm "$BRANCH_NAME"
fi

# 6. Branch & Tag Deletion
# git branch -d checks the current branch. This command runs from the
# orchestration checkout, so a branch whose tip is master still fails -d.
# Delete that name when the tip is already in refs/heads/master, and drop
# the start tag only after the branch is gone.
branch_deleted=0
if [ $FORCE_LEVEL -ge 2 ]; then
    echo "Force deleting branch '$BRANCH_NAME'..."
    if git branch -D "$BRANCH_NAME"; then
        branch_deleted=1
    fi
elif [ "$IS_MERGED" = true ] || [ "$HAS_UNIQUE_COMMITS" = false ]; then
    echo "Branch '$BRANCH_NAME' is contained in master. Deleting it."
    if git merge-base --is-ancestor "$BRANCH_NAME" HEAD 2>/dev/null; then
        git branch -d "$BRANCH_NAME" && branch_deleted=1
    else
        git branch -D "$BRANCH_NAME" && branch_deleted=1
    fi
    if [ "$branch_deleted" -ne 1 ] && [ $FORCE_LEVEL -ge 1 ]; then
        echo "Standard deletion failed. Force deleting branch..."
        git branch -D "$BRANCH_NAME" && branch_deleted=1
    fi
else
    echo "Leaving unmerged branch '$BRANCH_NAME' and its tag intact."
fi

if [ "$branch_deleted" -eq 1 ]; then
    git tag -d "${BRANCH_NAME}-start" 2>/dev/null || true
fi

echo "Cleanup complete."
