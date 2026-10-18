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
# Make the tree deletable by us, in this order, each step loud and fatal:
# 1. the root-owned ENGINEERING_LOG.md loses chattr +a (chown/chmod/unlink of an
#    append-only file is refused, even for root);
# 2. if anything in the tree is not ours (root-owned log, files of another agent
#    user), sudo chown -R gives it to us;
# 3. chmod -R u+rwX, so git worktree remove can unlink everything.
_me=$(id -un)
_log="$WORKTREE_DIR/ENGINEERING_LOG.md"
if [ -e "$_log" ] && [ ! -L "$_log" ]; then
    _attrs=$(lsattr -d -- "$_log") || {
        echo "Error: lsattr failed on $_log; cannot tell whether it is append-only." >&2; exit 1; }
    case "${_attrs%% *}" in
        *a*)
            echo "  $_log is append-only: sudo chattr -a"
            sudo chattr -a -- "$_log" || {
                echo "Error: sudo chattr -a $_log failed; the worktree cannot be removed." >&2; exit 1; }
            ;;
    esac
fi
_foreign=$(find "$WORKTREE_DIR" ! -user "$_me" -print -quit) || {
    echo "Error: could not scan $WORKTREE_DIR for files owned by other users." >&2; exit 1; }
if [ -n "$_foreign" ]; then
    echo "  Files not owned by $_me (first: $_foreign): sudo chown -R $_me:$(id -gn)"
    sudo chown -R -- "$_me:$(id -gn)" "$WORKTREE_DIR" || {
        echo "Error: sudo chown -R of $WORKTREE_DIR failed." >&2; exit 1; }
fi
echo "Resetting permissions on '$WORKTREE_DIR' for deletion..."
chmod -R u+rwX -- "$WORKTREE_DIR" || {
    echo "Error: chmod -R u+rwX $WORKTREE_DIR failed." >&2; exit 1; }

# third_party: remove library worktrees through their host, so the registration
# goes too (rm -rf alone left a dead entry and the next fetch-deps failed).
if [ -d "$WORKTREE_DIR/third_party" ]; then
  echo "Cleaning third_party/*/src under $WORKTREE_DIR ..."
  for _src in "$WORKTREE_DIR"/third_party/*/src; do
    [ -e "$_src" ] || continue
    if [ -f "$_src/.git" ]; then
      _hostgit=$(git -C "$_src" rev-parse --path-format=absolute --git-common-dir) || {
        echo "Error: $_src/.git names no readable library repo; fix it by hand." >&2; exit 1; }
      _host=$(dirname "$_hostgit")
      echo "  git -C $_host worktree remove --force $_src"
      git -C "$_host" worktree remove --force "$_src" || {
        echo "Error: could not remove library worktree $_src from $_host." >&2; exit 1; }
    fi
    if [ -e "$_src" ]; then
      rm -rf -- "$_src" || { echo "Error: rm -rf $_src failed." >&2; exit 1; }
    fi
  done
fi

echo "Removing worktree '$WORKTREE_DIR'..."
git worktree remove --force "$WORKTREE_DIR" || {
    echo "Error: git worktree remove --force $WORKTREE_DIR failed (message above)." >&2
    echo "  Nothing else was tried. Fix the cause and re-run." >&2
    exit 1
}

if [ -d "$WORKTREE_DIR" ]; then
    echo "Error: Failed to remove worktree directory."
    exit 1
fi
fi

# A folder deleted by hand leaves .git/worktrees/<id> behind; prune clears it.
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
    if git rev-parse -q --verify "refs/tags/${BRANCH_NAME}-start" >/dev/null; then
        git tag -d "${BRANCH_NAME}-start" || {
            echo "Error: could not delete tag ${BRANCH_NAME}-start." >&2; exit 1; }
    fi
fi

echo "Cleanup complete."
