#!/bin/bash
# setup_agent.sh: Automate creation of agent worktrees
# Usage (from orchestration root):
#   ./setup_agent.sh [--force] branch-name
#   source ./setup_agent.sh [--force] branch-name   # same end state
# --force: overwrite dest tracked seed files that differ from orch.
#
# On success: exec a new interactive shell in the new worktree with project
# environment (umask 002 + full groups via ve-refresh-shell, same as env).
# That replaces this terminal only (other GUI apps stay open).
#
# Always run from the orchestration root.
# Ensures the new worktree is fully permissioned (setgid dirs, log/wrapper,
# run-as-primary, ve-refresh-shell, etc.) so agents can start immediately.

SETUP_FORCE=0
BRANCH_NAME=""
while [ $# -gt 0 ]; do
    case "$1" in
        -f|--force) SETUP_FORCE=1; shift ;;
        -h|--help)
            echo "Usage: ./setup_agent.sh [--force] branch-name"
            echo "  --force  overwrite dest seed files that differ from orch"
            return 1 2>/dev/null || exit 1
            ;;
        *)
            if [ -z "$BRANCH_NAME" ]; then
                BRANCH_NAME="$1"
            else
                echo "Error: unexpected argument: $1"
                return 1 2>/dev/null || exit 1
            fi
            shift
            ;;
    esac
done

if [ -z "$BRANCH_NAME" ]; then
    echo "Usage: source ./setup_agent.sh [--force] branch-name   # or: ./setup_agent.sh [--force] branch-name"
    # When sourced, exit would kill the shell — use return if possible
    return 1 2>/dev/null || exit 1
fi

# So post-checkout is a no-op mid-setup (hook no longer runs fix-perms; still skip until seeded)
export VE_SETUP_AGENT=1
# shellcheck disable=SC2064
trap 'unset VE_SETUP_AGENT' EXIT

# Load KEY=VALUE from project.config. Keep equals — do not split on '='.
_setup_die() { echo "ERROR: $*" >&2; exit 1; }
if [ ! -f project.config ]; then
  _setup_die "missing project.config (required user/group keys; no dlang/ai-* defaults)"
fi
while IFS= read -r line || [[ -n "$line" ]]; do
  line="${line%%#*}"
  line="${line#"${line%%[![:space:]]*}"}"
  line="${line%"${line##*[![:space:]]}"}"
  [[ -z "$line" ]] && continue
  [[ "$line" =~ ^[a-zA-Z_][a-zA-Z0-9_]*= ]] || continue
  # shellcheck disable=SC2163
  export "$line"
done <project.config
_setup_req() {
  local n="$1" v="$2"
  if [ -z "$v" ] || [[ "$v" == @@* ]]; then
    _setup_die "project.config missing or unsmudged $n"
  fi
}
PRIMARY_USER=${primary_user:-}
CODE_GROUP=${code_group:-}
SHARED_GROUP=${shared_group:-}
PLANNING_USER=${planning_user:-}
CODER_USER=${coder_user:-}
ORCHESTRATOR_USER=${orchestrator_user:-}
_setup_req primary_user "$PRIMARY_USER"
_setup_req planning_user "$PLANNING_USER"
_setup_req coder_user "$CODER_USER"
_setup_req orchestrator_user "$ORCHESTRATOR_USER"
_setup_req code_group "$CODE_GROUP"
_setup_req shared_group "$SHARED_GROUP"

# Owners, groups, modes and umasks: perm-model.tsv via perm-lib.sh (orchestration root).
for _f in perm-lib.sh perm-engine.py perm-model.tsv ve-resolve-orch; do
  [ -f "$_f" ] || _setup_die "missing $(pwd)/$_f (run ./deploy-orchestration / update-rules.sh)"
done
# shellcheck source=/dev/null
. ./perm-lib.sh
perm_init "$(pwd)/project.config" || exit 1
# shellcheck source=/dev/null
. ./ve-resolve-orch
# The worktree area's umask (002): new dirs 2775 under the setgid root, files 664.
umask "$(perm_umask worktree)" || exit 1

# .git is mode 2770 and owned by the primary user, so this shell can enter it.
# Role accounts rewrite files such as .git/config as mode 660, group
# $SHARED_GROUP. A long-lived desktop shell does not have that supplementary
# group until `source ./env`.
if ! id -nG | grep -Fqw -- "$SHARED_GROUP"; then
  echo "Error: this shell is missing group $SHARED_GROUP." >&2
  echo "  .git/config is $(stat -c '%A %U:%G' .git/config 2>/dev/null || echo missing)." >&2
  echo "  Run: source ./env" >&2
  echo "  Then: ./setup_agent.sh $BRANCH_NAME" >&2
  exit 1
fi
if [ ! -r .git/config ]; then
  echo "Error: cannot read .git/config ($(stat -c '%A %U:%G' .git/config 2>/dev/null || echo missing))." >&2
  exit 1
fi

# A removed worktree can leave a prunable registration. The slot loop treats a
# missing directory as free, and git worktree add then refuses that path.
if ! git worktree prune -v; then
    echo "Error: git worktree prune failed." >&2
    exit 1
fi

# 0. Safety Check: Don't name a branch agent-N (reserved for auto-generated dirs)
if [[ "$BRANCH_NAME" =~ ^agent-[0-9]+$ ]]; then
    echo "Error: Branch name cannot be '$BRANCH_NAME' (reserved for directory names)."
    echo "Use a descriptive name like 'feature-x' instead."
    exit 1
fi

is_valid_worktree_dir() {
  local p="$1"
  [ -f "$p/.git" ] && grep -q '^gitdir: ' "$p/.git" 2>/dev/null
}

# 1. Determine next available agent-N ID (reuse slot if prior setup left a non-worktree stub)
N=1
while true; do
  if [ ! -d "agent-$N" ]; then
    AGENT_ID="agent-$N"
    break
  fi
  if is_valid_worktree_dir "agent-$N"; then
    N=$((N + 1))
    continue
  fi
  echo "Removing stale non-worktree directory agent-$N (failed prior setup_agent run)..."
  # Its log is root-owned and may be append-only, so the removal needs root.
  if [ -f "agent-$N/ENGINEERING_LOG.md" ]; then
    sudo chattr -a "agent-$N/ENGINEERING_LOG.md" || _setup_die "cannot lift chattr +a on agent-$N/ENGINEERING_LOG.md"
  fi
  sudo rm -rf -- "agent-$N" || _setup_die "cannot remove stale agent-$N (try: sudo rm -rf agent-$N)"
  AGENT_ID="agent-$N"
  break
done

# Safety: refuse if branch name would conflict with an existing directory (other than the agent dir we chose)
if [ -d "$BRANCH_NAME" ] && [ "$BRANCH_NAME" != "$AGENT_ID" ]; then
    echo "Error: A directory named '$BRANCH_NAME' already exists. Choose a different branch name to avoid conflict."
    exit 1
fi
if [ -e "${BRANCH_NAME}.wt" ] && [ ! -L "${BRANCH_NAME}.wt" ]; then
    echo "Error: '${BRANCH_NAME}.wt' exists but is not a symlink. Clean it up first or choose different name."
    exit 1
fi

# Ensure main .git/config is not root-owned (breaks git for users without active ai-shared GID)
if [ "$(stat -c '%U' .git/config)" = "root" ]; then
  echo "Repairing root-owned .git/config (requires sudo once)..."
  if ! sudo chown "$PRIMARY_USER:$SHARED_GROUP" .git/config; then
    echo "Error: .git/config is owned by root. Run:"
    echo "  sudo chown $PRIMARY_USER:$SHARED_GROUP .git/config"
    echo "  sudo ./fix-perms --verbose"
    exit 1
  fi
  perm_apply git-config .git/config --top-only || exit 1
fi

# Orchestration root (absolute) — used after cd into agent-N for seeding filters/config.
ORCH_ROOT="$(pwd)"

# Ensure shared git hooks are executable BEFORE worktree add / checkout.
# fix-perms must not assign ai-code to common .git; hooks no longer re-run fix-perms.
ensure_common_hooks_executable() {
  local common hooks
  common=$(git rev-parse --git-common-dir) || _setup_die "git rev-parse --git-common-dir failed in $(pwd)"
  case "$common" in
    /*) hooks="$common/hooks" ;;
    *) hooks="$ORCH_ROOT/$common/hooks" ;;
  esac
  [ -d "$hooks" ] || _setup_die "git hooks dir missing: $hooks"
  # One source: the tracked hooks/post-checkout
  [ -f "$ORCH_ROOT/hooks/post-checkout" ] || _setup_die "missing $ORCH_ROOT/hooks/post-checkout (run deploy-orchestration)"
  cp "$ORCH_ROOT/hooks/post-checkout" "$hooks/post-checkout" || _setup_die "cannot install $hooks/post-checkout"
  # Active hooks only (not *.sample); a hook this user cannot fix is an error
  perm_apply git-hooks "$hooks" --maxdepth 1 --skip-glob '*.sample' || _setup_die "cannot set hook modes in $hooks"
  [ -x "$hooks/post-checkout" ] || _setup_die "$hooks/post-checkout is not executable (check ownership)"
}

# sandbox_dir from the given project.config; fails if missing.
read_sandbox_dir() {
  local v
  [ -f "$1" ] || _setup_die "missing $1"
  v=$(sed -n 's/^sandbox_dir=//p' "$1" | tr -d '\r' | tail -1)
  [ -n "$v" ] && [[ "$v" != *@@* ]] || _setup_die "sandbox_dir has no value in $1"
  printf '%s' "$v"
}

ensure_common_hooks_executable

# 2. Create Worktree & Branch
echo "Creating worktree for $AGENT_ID on branch $BRANCH_NAME..."

WORKTREE_ERR=0
if git show-ref --verify --quiet "refs/heads/$BRANCH_NAME"; then
    git worktree add --no-checkout "$AGENT_ID" "$BRANCH_NAME" || WORKTREE_ERR=$?
else
    # Full ref. This checkout has a master/ directory and
    # refs/remotes/{origin,local,push}/master beside refs/heads/master.
    if ! git rev-parse --verify --quiet --end-of-options 'refs/heads/master^{commit}' >/dev/null; then
        echo "Error: refs/heads/master does not resolve to a commit." >&2
        git rev-parse --verify --end-of-options 'refs/heads/master^{commit}' >&2 || true
        exit 1
    fi
    git worktree add --no-checkout "$AGENT_ID" -b "$BRANCH_NAME" refs/heads/master || WORKTREE_ERR=$?
    if [ "$WORKTREE_ERR" -eq 0 ]; then
      # Create (or force-update) a lightweight tag for git describe to anchor on.
      echo "Creating/updating lightweight tag ${BRANCH_NAME}-start for versioning..."
      git tag -f "${BRANCH_NAME}-start" "$BRANCH_NAME" || WORKTREE_ERR=$?
    fi
fi

if [ "$WORKTREE_ERR" -ne 0 ]; then
    echo "Error: Failed to create worktree."
    exit 1
fi
# The planner is not in ai-code: the worktree dir needs other-search (worktree area,
# 2775). The owner sets it without sudo; do not wait for fix-perms.
perm_apply worktree "$AGENT_ID" --top-only || {
    echo "Error: cannot set the worktree mode on $AGENT_ID (planner will not be able to enter)."
    exit 1
}

# 3. Create convenience symlink for the branch
if [ -e "${BRANCH_NAME}.wt" ]; then
    if [ -L "${BRANCH_NAME}.wt" ] && [ "$(readlink "${BRANCH_NAME}.wt")" = "$AGENT_ID" ]; then
        echo "Symlink '${BRANCH_NAME}.wt' already points correctly. Skipping."
    else
        echo "Warning: '${BRANCH_NAME}.wt' exists but is not the correct symlink. Removing and recreating."
        rm -f "${BRANCH_NAME}.wt"
        ln -s "$AGENT_ID" "${BRANCH_NAME}.wt"
        echo "Created symlink: ${BRANCH_NAME}.wt -> $AGENT_ID"
    fi
else
    ln -s "$AGENT_ID" "${BRANCH_NAME}.wt"
    echo "Created symlink: ${BRANCH_NAME}.wt -> $AGENT_ID"
fi

# Copy orch seed only if dest missing, identical, or SETUP_FORCE=1.
# After checkout, dest is the branch file — do not silently clobber worktree-ahead.
copy_orch_seed_file() {
  local src="$1" dest="$2"
  if [ ! -f "$src" ]; then
    return 0
  fi
  if [ ! -f "$dest" ]; then
    cp "$src" "$dest"
    return 0
  fi
  if cmp -s "$src" "$dest"; then
    return 0
  fi
  if [ "${SETUP_FORCE:-0}" -eq 1 ]; then
    echo "  seed: FORCE overwrite $dest"
    cp "$src" "$dest"
    return 0
  fi
  echo "  SKIP seed $dest — differs from orch (pass --force to overwrite)"
}

seed_smudge_inputs() {
  local orch_root="$1"
  if [ -f "$orch_root/project.config" ]; then
    cp "$orch_root/project.config" ./project.config
  fi
  for f in filter-apply-config filter-clean-config ve-resolve-orch; do
    copy_orch_seed_file "$orch_root/$f" "./$f"
  done
  # ve-resolve-orch was sourced from the orchestration root at the start.
  ve_upsert_project_config_key ./project.config orch_root "$orch_root"
}


# Tracked paths whose .gitattributes say filter=manage-configs (none is an error).
manage_configs_files() {
  local listed
  listed=$(git ls-files | git check-attr --stdin filter \
    | awk -F': ' '$3 == "manage-configs" { print $1 }')
  if [ -z "$listed" ]; then
    echo "Error: .gitattributes names no filter=manage-configs files" >&2
    return 1
  fi
  printf '%s\n' "$listed"
}

re_smudge_stamped_files() {
  # Apply orchestration's filter script directly (do not use git checkout smudge:
  # the branch may ship an older broken filter-apply-config that git would invoke).
  local f mode tmp
  if [ ! -f ./filter-apply-config ]; then
    echo "Error: filter-apply-config missing after seeding from $ORCH_ROOT"
    return 1
  fi
  if grep -q '\${!CONF_\${' ./filter-apply-config 2>/dev/null; then
    echo "Error: filter-apply-config at $ORCH_ROOT still has broken indirect expansion."
    echo "  Commit the fix on orchestration, then retry."
    return 1
  fi
  mapfile -t _mc_files < <(manage_configs_files)
  for f in "${_mc_files[@]}"; do
    if ! git cat-file -e "HEAD:$f" 2>/dev/null; then
      continue
    fi
    tmp="$(mktemp)"
    if ! git show "HEAD:$f" | bash ./filter-apply-config > "$tmp"; then
      echo "Error: manual smudge failed for $f"
      rm -f "$tmp"
      return 1
    fi
    mv "$tmp" "$f"
    mode=$(git ls-tree HEAD "$f" | awk '{print $1}')
    case "$mode" in
      100755|100775) chmod a+x "$f" ;;
    esac
    # mktemp made it 0600: give it the worktree area's mode like every checked-out file
    perm_apply worktree "$f" --top-only || return 1
  done
}

# Smudge rewrites tracked files in place. The clean filter turns those bytes
# back into the index blob, so `git diff` is empty, but `git status` still
# reports them modified because size and mtime changed. `git add` stores the
# new stat. It does not stage a content change when the cleaned blob matches.
refresh_smudged_index() {
  local f
  local -a present=()
  mapfile -t _mc_files < <(manage_configs_files)
  for f in "${_mc_files[@]}"; do
    [ -f "$f" ] || continue
    present+=("$f")
  done
  if [ "${#present[@]}" -eq 0 ]; then
    return 0
  fi
  git add -- "${present[@]}"
}

# Filter config + every filtered file smudged (orchestration's checker; cwd = this worktree).
verify_smudge() {
  "$ORCH_ROOT/verify-smudge"
}

# A failed populate removes the new worktree (from the orchestration root). If that removal
# fails too, its error is shown with the command to finish by hand.
abort_new_worktree() {
  cd "$ORCH_ROOT" || _setup_die "cannot cd to $ORCH_ROOT"
  if ! git worktree remove --force "$AGENT_ID"; then
    echo "Error: could not remove the half-made worktree $AGENT_ID; remove it with:" >&2
    echo "  git worktree remove --force $AGENT_ID" >&2
  fi
  return 1 2>/dev/null || exit 1
}

# 4. Populate worktree from branch tip, then manually smudge stamped files.
cd "$AGENT_ID"

# project.config is gitignored — seed BEFORE checkout so post-checkout (if it runs)
# and any early tooling see it. Re-seed after checkout in case filters were overwritten.
if [ ! -f "$ORCH_ROOT/project.config" ]; then
  echo "Error: $ORCH_ROOT/project.config missing."
  echo "  Copy project.config.example → project.config and fill local values, then retry."
  abort_new_worktree
fi
seed_smudge_inputs "$ORCH_ROOT"

# After --no-checkout the index is empty; 'git checkout .' matches nothing.
# Bypass git smudge on bulk checkout — feature branches may carry broken filter scripts.
# VE_SETUP_AGENT=1 makes post-checkout a no-op during worktree populate.
if ! git -c filter.manage-configs.smudge=cat -c filter.manage-configs.clean=cat checkout HEAD -- .; then
  echo "Error: Failed to populate worktree (git checkout HEAD -- .)."
  abort_new_worktree
fi

# Re-seed filters from orchestration (authoritative) then smudge stamped paths.
seed_smudge_inputs "$ORCH_ROOT"
if ! re_smudge_stamped_files; then
  echo "Error: Failed to smudge stamped files using orchestration filter scripts."
  abort_new_worktree
fi
if ! refresh_smudged_index; then
  echo "Error: Failed to refresh the index after smudge."
  abort_new_worktree
fi

if ! verify_smudge; then
  echo "Hint: ensure orchestration root has project.config and working filter-apply-config."
  abort_new_worktree
fi

if [ "$(id -un)" != "$PRIMARY_USER" ] && [ "$EUID" -ne 0 ]; then
  sudo -u "$PRIMARY_USER" git config core.sharedRepository group
else
  git config core.sharedRepository group
fi

# 5. Setup Agent Workspace Folders
mkdir -p .gemini/policies
mkdir -p .gemini/plans
touch .gemini/plans/.gitkeep

# 6. Setup Sandbox (symlink named by project.config sandbox_dir)
SANDBOX_DIR="$(read_sandbox_dir ./project.config)" || exit 1
if [ "$SANDBOX_DIR" != "dev-ai-interaction" ] && [ -L "dev-ai-interaction" ]; then
  rm -f "dev-ai-interaction"
fi
if [ "$SANDBOX_DIR" != "sandbox" ] && [ -L "sandbox" ]; then
  rm -f "sandbox"
fi
if [ -L "$SANDBOX_DIR" ] || [ ! -e "$SANDBOX_DIR" ]; then
  ln -sfn "../$SANDBOX_DIR" "$SANDBOX_DIR"
else
  echo "Sandbox $SANDBOX_DIR already exists and is not a symlink. Leaving it."
fi

# 6b. Copy local.properties for Android builds
if [ -f "../master/local.properties" ]; then
    cp ../master/local.properties local.properties
elif [ -f "../local.properties" ]; then
    cp ../local.properties local.properties
fi

# 7. Identity and fork anchor. exec-preflight reads "- **Agent ID:** " (plans' Worktree:);
# Start-branch owns the -start tag (git describe); Forked-from is the parent branch name.
[[ -n "${AGENT_ID:-}" ]] || _setup_die "AGENT_ID is empty; cannot write AGENT_CONTEXT.md"
cat > AGENT_CONTEXT.md <<EOF
- **Agent ID:** $AGENT_ID
- **Current Branch:** $BRANCH_NAME
Forked-from: master
Start-branch: $BRANCH_NAME
EOF
_ctx_id="$(sed -n 's/^- \*\*Agent ID:\*\* //p' AGENT_CONTEXT.md | head -1 | tr -d '\r')"
[[ "$_ctx_id" == "$AGENT_ID" ]] || _setup_die "AGENT_CONTEXT.md Agent ID is '$_ctx_id', expected '$AGENT_ID'"

# 8. Make this a *fully working* tree with all correct permissions.
#    Since we are always run from the orchestration root (which has the
#    authoritative latest copies and fixers), we:
#    - copy latest critical infra files
#    - build the setuid helper
#    - run the standard permission fixers (via sudo for root:ai-shared parts
#      like ENGINEERING_LOG.md ownership + chattr +a, and wrapper)
#    This ensures the new agent can immediately use append-to-engineering-log,
#    run-as-primary re-exec, correct setgid, etc. without extra steps.
AGENT_ABS=$(pwd)
PARENT_ROOT=".."

# Copy latest authoritative copies of key permission/infra files from orchestration root
# (ensures even if the branch tip was slightly behind, the tree is current)
for f in append-to-engineering-log run-as-primary.c env ve-refresh-shell.c; do
  copy_orch_seed_file "$PARENT_ROOT/$f" "$AGENT_ABS/$f"
done
# Launchers often live only on orchestration until update-rules; seed common ones
for f in run-grok-coder run-grok-orchestrator run-grok-master run-grok-planner; do
  if [ -f "$PARENT_ROOT/$f" ] && [ ! -f "$AGENT_ABS/$f" ]; then
    cp -p "$PARENT_ROOT/$f" "$AGENT_ABS/$f" || _setup_die "cannot copy $f into $AGENT_ABS"
    ve_add_other_x "$AGENT_ABS/$f" || exit 1
  fi
done

# Build / ensure the setuid helper (run-as-primary) is present and correct
if [ -f "$AGENT_ABS/run-as-primary.c" ]; then
  (cd "$AGENT_ABS" && gcc -O2 -Wall -o run-as-primary run-as-primary.c) || _setup_die "gcc failed for $AGENT_ABS/run-as-primary.c"
  perm_apply setuid-run-as-primary "$AGENT_ABS/run-as-primary" --top-only || exit 1
fi

# Create log file with minimal header if missing (fixer will harden it)
if [ ! -f "$AGENT_ABS/ENGINEERING_LOG.md" ]; then
  echo "## $(date +%Y-%m-%d) - Initial log for $AGENT_ID" > "$AGENT_ABS/ENGINEERING_LOG.md" \
    || _setup_die "cannot create $AGENT_ABS/ENGINEERING_LOG.md"
fi

cd "$ORCH_ROOT" || _setup_die "cannot cd to $ORCH_ROOT"

# Planner search on the worktree dir (again after populate). Owner chmod; no sudo.
perm_apply worktree "$AGENT_ABS" --top-only || {
    echo "Error: cannot set the worktree mode on $AGENT_ABS (planner will not be able to enter)."
    exit 1
}

# fix-perms for the new tree only (it never enters another worktree or the sandbox).
echo "Running sudo ./fix-perms $AGENT_ABS"
if ! sudo ./fix-perms "$AGENT_ABS"; then
    _setup_die "sudo ./fix-perms $AGENT_ABS failed (output above). Fix the cause, then re-run: sudo ./fix-perms $AGENT_ABS"
fi
if [ -f "$AGENT_ABS/run-as-primary" ]; then
  perm_verify setuid-run-as-primary "$AGENT_ABS/run-as-primary" --top-only || _setup_die "run-as-primary is not as perm-model.tsv says after fix-perms"
fi

# ve-refresh-shell: build + setuid-root in orch AND this agent worktree (not in git).
# One sudo password may be requested. Never leave setuid on non-root owner.
# Setuid root needs root, so the installer always runs under sudo; a failure stops setup.
if [ -x "$ORCH_ROOT/install-ve-refresh-shell.sh" ]; then
  echo "Installing ve-refresh-shell (setuid root) in orch + worktree..."
  for _dest in "$ORCH_ROOT" "$AGENT_ABS"; do
    sudo "$ORCH_ROOT/install-ve-refresh-shell.sh" "$_dest" || _setup_die "install-ve-refresh-shell.sh $_dest failed"
  done
else
  # Projects without the installer: build here, then root owns it (setuid-refresh-shell area).
  for _dest in "$ORCH_ROOT" "$AGENT_ABS"; do
    [ -f "$_dest/ve-refresh-shell.c" ] || continue
    (cd "$_dest" && gcc -O2 -Wall -o ve-refresh-shell ve-refresh-shell.c) || _setup_die "gcc failed for $_dest/ve-refresh-shell.c"
    sudo chown root:root "$_dest/ve-refresh-shell" || _setup_die "cannot chown root:root $_dest/ve-refresh-shell"
    sudo chmod 4755 "$_dest/ve-refresh-shell" || _setup_die "cannot chmod 4755 $_dest/ve-refresh-shell"
  done
fi

# After fix-perms, re-assert hooks (defense in depth; post-checkout must stay runnable)
ensure_common_hooks_executable

# Merge drivers are repo-global (.git config); ensure installed for all worktrees
if [ -x "$ORCH_ROOT/install-merge-drivers.sh" ]; then
  (cd "$ORCH_ROOT" && ./install-merge-drivers.sh >/dev/null) || _setup_die "install-merge-drivers.sh failed"
fi
for _f in "$AGENT_ABS"/git-merge-drivers/* "$AGENT_ABS/install-merge-drivers.sh" "$AGENT_ABS/merge-branch-into-master.sh"; do
  if [ -f "$_f" ]; then
    ve_add_other_x "$_f" || exit 1
  fi
done
ve_ensure_tracked_exec_other_x "$AGENT_ABS" || exit 1

# 9. third_party comes from the pinned, committed artifacts in the checkout; setup never checks out
#    or builds a library (that is fetch-deps rw/upgrade/build, by hand). Every [[artifact]] path a
#    libpin.toml declares must exist (pin-local, app/..., or absolute), else stop.
python3 - "$AGENT_ABS" <<'PYEND_ARTIFACTS' || _setup_die "pinned third_party artifacts missing (list above); commit them on the branch (fetch-deps build NAME in a tree that has the library)"
import glob, os, re, sys
root = sys.argv[1]
missing, n = [], 0
for pin in sorted(glob.glob(os.path.join(root, "third_party/*/libpin.toml"))):
    lib = os.path.dirname(pin)
    for blk in re.split(r"(?m)^\[\[artifact\]\]\s*$", open(pin).read())[1:]:
        m = re.search(r'(?m)^path\s*=\s*"([^"]+)"', blk)
        if not m:
            missing.append("%s: [[artifact]] without path" % os.path.relpath(pin, root))
            continue
        p = m.group(1)
        n += 1
        full = p if os.path.isabs(p) else os.path.join(root, p) if p.startswith("app/") else os.path.join(lib, p)
        if not os.path.exists(full):
            missing.append(os.path.relpath(full, root))
            continue
        h = re.search(r'(?m)^sha256\s*=\s*"([0-9a-f]{64})"', blk)
        if h:
            import hashlib
            got = hashlib.sha256(open(full, "rb").read()).hexdigest()
            if got != h.group(1):
                missing.append("%s (sha256 %s, pinned %s)" % (os.path.relpath(full, root), got, h.group(1)))
print("third_party: %d pinned artifact(s) present" % (n - len(missing)))
for x in missing:
    print("ERROR: missing pinned artifact " + x, file=sys.stderr)
sys.exit(1 if missing else 0)
PYEND_ARTIFACTS

# CRITICAL: seed/smudge/copy of tracked files must not leave the agent worktree dirty,
# or ./build_app will refuse (uncommitted tracked files gate). Commit if needed.
(
  set -e
  cd "$AGENT_ABS"
  if ! git diff --quiet || ! git diff --cached --quiet; then
    echo "Committing setup_agent tracked-file updates so build_app can run..."
    git add -u
    # Newly copied tracked scripts may be untracked if not on branch tip yet (only paths
    # that exist are named, so git add has nothing to complain about)
    _seed=()
    for _p in env deploy build_app setup_agent.sh fix-perms update-rules.sh \
              append-to-engineering-log get-builds-tag.sh install-*.sh \
              git-merge-drivers hooks AGENT_MANDATES.md AGENTS.md GROK.md \
              standard-plan-compliance-block.md project-facts.md; do
      if [ -e "$_p" ]; then _seed+=("$_p"); fi
    done
    if [ "${#_seed[@]}" -gt 0 ]; then
      git add -A -- "${_seed[@]}"
    fi
    if ! git diff --cached --quiet; then
      git commit -m "chore: setup_agent seed/sync tracked infra (keep build_app clean)"
    fi
  fi
) || _setup_die "committing setup_agent's tracked-file updates in $AGENT_ABS failed (output above)"

# --- Enter new worktree shell (env semantics) ---
# Replace this process with an interactive shell in AGENT_ABS, umask 002, full
# project groups when ve-refresh-shell is installed setuid (same as source ./env).
unset VE_SETUP_AGENT
trap - EXIT

echo "Worktree ready: $AGENT_ABS"
echo "Branch: $BRANCH_NAME  Agent ID: $AGENT_ID"
echo "Next: run a launcher from here, e.g.  ../run-grok-coder   or   ../run-grok-planner"
echo "Entering worktree shell (umask 002 + project groups via env helper)..."

export VE_ENV_CWD="$AGENT_ABS"
umask "$(perm_umask worktree)"

# Prefer setuid-root helper only (setuid+non-root owner is unsafe — see env)
REFRESH=""
for _vrs in "$ORCH_ROOT/ve-refresh-shell" "$AGENT_ABS/ve-refresh-shell"; do
  if [ -x "$_vrs" ] && [ -u "$_vrs" ] && [ "$(stat -c '%U' "$_vrs" 2>/dev/null)" = "root" ]; then
    REFRESH="$_vrs"
    break
  fi
done

if [ -n "$REFRESH" ]; then
  exec "$REFRESH"
fi

# No setuid helper yet: plain interactive shell in the worktree (umask 002; groups may be stale)
echo "NOTE: ve-refresh-shell not setuid yet — shell may lack project groups."
echo "  One-time: gcc -O2 -Wall -o ve-refresh-shell ve-refresh-shell.c && sudo chown root:root ve-refresh-shell && sudo chmod 4755 ve-refresh-shell"
echo "  Then: source ./env"
cd "$AGENT_ABS" || {
  echo "ERROR: cannot cd to $AGENT_ABS"
  return 1 2>/dev/null || exit 1
}
SHELL_BIN="${SHELL:-/bin/bash}"
exec "$SHELL_BIN"
