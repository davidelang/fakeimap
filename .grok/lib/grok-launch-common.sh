# grok-launch-common.sh — shared machinery for run-grok* and run-cursor* (no project law).
# Install at: <repo>/.grok/lib/grok-launch-common.sh
# Thin wrappers set GROK_LAUNCHER_DIR + ROLE_KEY, then source this and call
# grok_launch_main "$@" (grok) or cursor_launch_main "$@" (cursor-agent; see
# .cursor/lib/cursor-launch-common.sh).
# Grok confinement is a custom sandbox profile (extends read-only). Cursor still
# uses agent-landlock; Grok's profile does not apply to cursor-agent.
#
# Rules: AGENTS.md (auto-loaded) + roles/<ROLE_KEY>.md (grok: --rules; cursor: sessionStart
# hook in .cursor/hooks.json); settings in roles.config (cursor: cursor_model=)
# Wiring: project.config (users, sandbox_dir, optional *_model)
#
# shellcheck shell=bash

# Memory cap and early OOM kill. run-antigravity* source this file with
# AGENT_OOM_FUNCS_ONLY=1 and return here, before launcher setup.
# Cgroup cap: systemd-run --user --scope MemoryMax, MemorySwapMax=0.
# No user bus: warn, and still set oom_score_adj (children inherit it).
# OOM score by ROLE_KEY: coder 1000, master 999, planner 998, every other
# role 997. AGENT_OOM_SCORE_ADJ overrides. A process may raise its own
# score; only CAP_SYS_RESOURCE can lower it.
# *_MEMORY_MAX: 0 disables the cap. Unset grok coder is 0 (the build shares
# this cgroup); unset cursor coder is 8G; every other role is 4G.

agent_oom_default() {
  case "$ROLE_KEY" in
    coder) printf '%s' 1000 ;;
    master) printf '%s' 999 ;;
    planner) printf '%s' 998 ;;
    *) printf '%s' 997 ;;
  esac
}

agent_oom_adj() {
  local adj="${AGENT_OOM_SCORE_ADJ:-}"
  if [[ -n "$adj" && ! "$adj" =~ ^-?[0-9]+$ ]]; then
    echo "WARN: ignoring invalid AGENT_OOM_SCORE_ADJ=${adj}" >&2
    adj=""
  fi
  if [[ -z "$adj" ]]; then
    adj="$(agent_oom_default)"
  fi
  if (( adj > 1000 )); then
    adj=1000
  fi
  if (( adj < -1000 )); then
    adj=-1000
  fi
  printf '%s' "$adj"
}

# $1 variable name, $2 default. Digits plus optional K/M/G/T. A zero value is 0.
agent_memory_max() {
  local name="$1"
  local def="$2"
  local val=""
  # Separate from local: one local line expands ${!name} before name is set.
  if [[ "$name" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
    val="${!name-}"
  else
    echo "WARN: ignoring memory setting; bad variable name ${name}" >&2
    printf '%s' "$def"
    return 0
  fi
  if [[ -n "$val" ]]; then
    if [[ "$val" =~ ^0+[KMGT]?$ ]]; then
      printf '%s' 0
      return 0
    fi
    if [[ "$val" =~ ^[0-9]+[KMGT]?$ ]]; then
      printf '%s' "$val"
      return 0
    fi
    echo "WARN: ignoring invalid ${name}=${val}" >&2
  fi
  printf '%s' "$def"
}

grok_memory_max() {
  local def=4G
  if [[ "${ROLE_KEY:-}" == coder ]]; then
    def=0
  fi
  agent_memory_max GROK_MEMORY_MAX "$def"
}

cursor_memory_max() {
  local def=4G
  if [[ "${ROLE_KEY:-}" == coder ]]; then
    def=8G
  fi
  agent_memory_max CURSOR_MEMORY_MAX "$def"
}

# bash -c script: set oom_score_adj, optional umask, then exec "$@".
role_exec_bash() {
  local umask_arg="${1:-}" adj
  adj="$(agent_oom_adj)"
  # Group redirect: a denied /proc write must not print or abort the launch.
  if [[ -n "$umask_arg" ]]; then
    printf '{ echo %s >/proc/self/oom_score_adj; } 2>/dev/null || true; umask %s; exec "$@"' \
      "$adj" "$umask_arg"
  else
    printf '{ echo %s >/proc/self/oom_score_adj; } 2>/dev/null || true; exec "$@"' "$adj"
  fi
}

systemd_user_scope_ok() {
  command -v systemd-run >/dev/null 2>&1 || return 1
  systemd-run --user --scope -q --collect -d \
    -p MemoryMax=64M -p MemorySwapMax=0 -p OOMScoreAdjust=1000 \
    -- true >/dev/null 2>&1
}

# $1 mem_max (empty or 0 = no cgroup cap). The rest is the command.
start_role_process() {
  local mem_max="$1"
  shift
  local adj
  adj="$(agent_oom_adj)"
  if [[ -n "$mem_max" && "$mem_max" != "0" ]] && systemd_user_scope_ok; then
    echo "MemoryMax=${mem_max} MemorySwapMax=0 OOMScoreAdjust=${adj}"
    # exec only returns when systemd-run itself cannot be started.
    if ! exec systemd-run --user --scope -q --collect -d \
      -p "MemoryMax=${mem_max}" \
      -p MemorySwapMax=0 \
      -p "OOMScoreAdjust=${adj}" \
      -- "$@"; then
      echo "WARN: systemd-run failed to start; OOM score ${adj} only, no MemoryMax=${mem_max}" >&2
    fi
  elif [[ -n "$mem_max" && "$mem_max" != "0" ]]; then
    echo "WARN: no systemd user bus; OOM score ${adj} only, no MemoryMax=${mem_max}" >&2
  else
    echo "OOMScoreAdjust=${adj} (no MemoryMax)"
  fi
  exec "$@"
}

if [[ "${AGENT_OOM_FUNCS_ONLY:-}" == 1 ]]; then
  return 0
fi

set -euo pipefail

if [[ -z "${GROK_LAUNCHER_DIR:-}" ]]; then
  echo "ERROR: GROK_LAUNCHER_DIR must be set to the repository worktree root" >&2
  exit 2
fi
SCRIPT_DIR="$(cd "$GROK_LAUNCHER_DIR" && pwd)"
if [[ -z "${ROLE_KEY:-}" ]]; then
  echo "ERROR: ROLE_KEY must be set by the run-* launcher" >&2
  exit 2
fi

if [[ ! -f "$SCRIPT_DIR/project.config" ]]; then
  echo "ERROR: $SCRIPT_DIR/project.config missing (copy project.config.example and fill it)" >&2
  exit 1
fi
{
  # KEY=VALUE lines only (skip comments/blank). Keep equals — do not split on '='.
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line%%#*}"
    line="${line#"${line%%[![:space:]]*}"}"
    line="${line%"${line##*[![:space:]]}"}"
    [[ -z "$line" ]] && continue
    [[ "$line" =~ ^[a-zA-Z_][a-zA-Z0-9_]*= ]] || continue
    # shellcheck disable=SC2163
    export "$line"
  done <"$SCRIPT_DIR/project.config"
}

# need_cfg KEY: value of a project.config key; exits with a clear error if unset or a @@TOKEN@@.
need_cfg() {
  local v="${!1:-}"
  if [[ -z "$v" || "$v" == @@* ]]; then
    echo "ERROR: $1 has no value in $SCRIPT_DIR/project.config" >&2
    exit 1
  fi
  printf '%s' "$v"
}

# project.config may write $HOME/… as literal text: expand after load.
_expand_path() {
  local p="$1"
  p="${p/#\~/$HOME}"
  if [[ "$p" == *'$HOME'* ]]; then
    p="${p//\$HOME/$HOME}"
  fi
  printf '%s' "$p"
}
# Binaries: grok_bin_default / cursor_bin, checked when that agent launches.
# Shared host-clone root (absolute) for third_party / landlock.
GIT_HOME="$(need_cfg git_home)"
GIT_HOME="$(_expand_path "$GIT_HOME")"
[[ "$GIT_HOME" = /* && -d "$GIT_HOME" ]] || { echo "ERROR: git_home=$GIT_HOME in project.config is not an absolute directory" >&2; exit 1; }
export GIT_HOME

# Sandbox: project.config sandbox_dir only (relative to this worktree, or absolute).
SANDBOX_REL="$(need_cfg sandbox_dir)"
if [[ "$SANDBOX_REL" = /* ]]; then
  SANDBOX_DIR="$SANDBOX_REL"
else
  SANDBOX_DIR="$SCRIPT_DIR/$SANDBOX_REL"
fi
[[ -d "$SANDBOX_DIR" ]] || { echo "ERROR: sandbox_dir=$SANDBOX_REL: $SANDBOX_DIR is not a directory" >&2; exit 1; }
SANDBOX_DIR="$(cd "$SANDBOX_DIR" && pwd)"


ANDROID_SHARED=""
if [[ ! -f "$SCRIPT_DIR/ve-resolve-orch" ]]; then
  echo "ERROR: missing $SCRIPT_DIR/ve-resolve-orch (run ./update-rules.sh from orchestration root)." >&2
  exit 1
fi
# shellcheck source=/dev/null
. "$SCRIPT_DIR/ve-resolve-orch"
ve_resolve_orch "$SCRIPT_DIR" || exit 1
ve_setup_worktree_build_homes "$SCRIPT_DIR" || exit 1
ANDROID_SHARED="$ANDROID_USER_HOME"
export ORCH_ROOT

LAUNCH_UMASK=002
umask "$LAUNCH_UMASK"

PACK_PATHS=()
EXTRA_ARGS=()
MODEL_ARGS=()
TODO_GATE_FLAGS=()
PROMPT_FILE=""
PROMPT=""

# Role -> project.config user key. master runs as coder_user.
resolve_run_user() {
  case "$ROLE_KEY" in
    planner) need_cfg planning_user ;;
    coder|master) need_cfg coder_user ;;
    orchestrator) need_cfg orchestrator_user ;;
    primary) need_cfg primary_user ;;
    *) echo "ERROR: unknown ROLE_KEY=$ROLE_KEY" >&2; exit 1 ;;
  esac
}

build_model_args() {
  MODEL_ARGS=()
  if [[ -n "${FORCE_MODEL:-}" ]]; then
    MODEL_ARGS=(--model "$FORCE_MODEL")
    return 0
  fi
  if [[ -n "${GROK_FORCE_MODEL:-}" ]]; then
    MODEL_ARGS=(--model "$GROK_FORCE_MODEL")
    return 0
  fi
  local v=""
  case "$ROLE_KEY" in
    planner) v="${planner_model:-${GROK_PLANNER_MODEL:-}}" ;;
    coder) v="${coder_model:-}" ;;
    master) v="${master_model:-}" ;;
    orchestrator) v="${orchestrator_model:-}" ;;
    primary) v="${primary_model:-}" ;;
  esac
  # IMPORTANT: with set -e, a failing `[[ ]] && cmd` as the last statement of a
  # function returns non-zero and aborts the launcher silently. Always return 0.
  [[ -z "$v" ]] && v="$(role_cfg model)"
  if [[ -n "$v" ]]; then
    MODEL_ARGS=(--model "$v")
  fi
  return 0
}

build_todo_gate_flags() {
  TODO_GATE_FLAGS=()
  if [[ "${GROK_TODO_GATE:-0}" = "1" || "${GROK_TODO_GATE:-0}" = "true" ]]; then
    TODO_GATE_FLAGS=(--todo-gate)
  fi
  return 0
}

collect_extra_args() {
  EXTRA_ARGS=()
  local found=0 a
  for a in "$@"; do
    if [[ "$found" -eq 1 ]]; then
      EXTRA_ARGS+=("$a")
    elif [[ "$a" = "--" ]]; then
      found=1
    fi
  done
}

# Do not cat a sandbox file or an argument path.
prepare_prompt() {
  PROMPT_FILE=""
  PACK_PATHS=()
  collect_extra_args "$@"
  # Startup prompt is the short new_agent_prompt; the role card goes in via --rules.
  local start="$SCRIPT_DIR/new_agent_prompt"
  if [[ ! -f "$start" ]]; then
    echo "ERROR: missing $start" >&2
    exit 1
  fi
  PROMPT_FILE="$start"
  PROMPT="$(cat "$start")"
  ROLE_CARD="$SCRIPT_DIR/roles/$ROLE_KEY.md"
  if [[ ! -f "$ROLE_CARD" ]]; then
    echo "ERROR: missing role card: $ROLE_CARD" >&2
    exit 1
  fi
}

# role_cfg KEY: value of KEY for ROLE_KEY in roles.config (blank if unset).
role_cfg() {
  local key="$1" f="$SCRIPT_DIR/roles.config" line tok
  [[ -f "$f" ]] || { echo "ERROR: missing $f" >&2; exit 1; }
  line="$(grep -E "^${ROLE_KEY}[[:space:]]" "$f" | head -1 || true)"
  for tok in $line; do
    [[ "$tok" == "$key="* ]] && { printf '%s' "${tok#*=}"; return 0; }
  done
  return 0
}

# build_landlock_args RUN_USER: sets LANDLOCK_ARGS (agent-landlock prefix) for any agent CLI.
# Mutation-only Landlock as the role user, immediately before the agent binary.
# Missing helper is an error. Helper itself honors AGENT_LANDLOCK_DISABLE=1 (explicit opt-out).
# Owner -x is not enough: ai-planner is not in ai-code; 774 → Permission denied.
LANDLOCK_ARGS=()
build_landlock_args() {
  local run_user="$1"
  local landlock_helper="${SCRIPT_DIR}/agent-landlock"
  LANDLOCK_ARGS=()
  if [[ ! -f "$landlock_helper" ]]; then
    echo "ERROR: $landlock_helper missing; refusing to launch without session Landlock" >&2
    exit 1
  fi
  {
    if ! sudo -u "$run_user" -- test -x "$landlock_helper"; then
      echo "ERROR: $run_user cannot execute $landlock_helper" >&2
      echo "  $(stat -c 'mode=%a owner=%U:%G' "$landlock_helper" 2>/dev/null || ls -la "$landlock_helper")" >&2
      echo "  $run_user groups: $(id -nG "$run_user" 2>/dev/null || true)" >&2
      echo "  Need other-execute (775) when helper is :ai-code and the role is not in ai-code." >&2
      echo "  Fix: chmod a+x $landlock_helper  (or run fix-perms / update-rules on this worktree)" >&2
      exit 1
    fi
    LANDLOCK_ARGS=(
      "$landlock_helper"
      --role "$ROLE_KEY"
      --worktree "$SCRIPT_DIR"
      --
    )
    echo "Landlock: $landlock_helper role=$ROLE_KEY"
  }
  return 0
}

# Grok sandbox profile for this launch. Cursor does not use this.
# extends=read-only so the worktree is not writable unless listed, and so Grok
# write-denies hook and config files. restrict_network=false keeps child-process
# network, matching agent-landlock (the read-only base would block it).
# read_write is directories only. /dev is the directory stand-in for the device
# nodes agent-landlock granted as files (/dev/null and the rest); Unix mode still
# blocks disks and memory devices. A same-named profile in the role user's
# ~/.grok/sandbox.toml would override this file, so each launch uses a fresh name.
SANDBOX_GRANT_DIRS=()
append_grant_dir() {
  local p="$1" resolved have
  [[ -n "$p" && -d "$p" ]] || return 0
  # Canonicalize when we can enter the directory. A role home runtime dir
  # (mode 700) still gets granted when this launcher cannot cd into it.
  if resolved="$(cd "$p" 2>/dev/null && pwd)"; then
    p="$resolved"
  fi
  for have in "${SANDBOX_GRANT_DIRS[@]+"${SANDBOX_GRANT_DIRS[@]}"}"; do
    [[ "$have" == "$p" ]] && return 0
  done
  SANDBOX_GRANT_DIRS+=("$p")
}

role_home_of() {
  local u="$1" h
  h="$(getent passwd "$u" | awk -F: 'NR==1{print $6}')"
  if [[ -z "$h" || ! -d "$h" ]]; then
    echo "ERROR: no home directory for user $u" >&2
    exit 1
  fi
  printf '%s' "$h"
}

# landlock.config profile.* path= lines. env:HOME is the role user's home.
load_landlock_profile_dirs() {
  local home="$1"
  local f="$SCRIPT_DIR/landlock.config"
  [[ -f "$f" ]] || { echo "ERROR: missing $f" >&2; exit 1; }
  local section="" line val rest var sub base
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line%%#*}"
    line="${line#"${line%%[![:space:]]*}"}"
    line="${line%"${line##*[![:space:]]}"}"
    [[ -z "$line" ]] && continue
    if [[ "$line" == \[*\] ]]; then
      section="${line#"["}"
      section="${section%"]"}"
      continue
    fi
    val=""
    if [[ "$section" == profile.* && "$line" == path=* ]]; then
      val="${line#path=}"
    elif [[ "$line" == *_paths=* ]]; then
      rest="${line#*=}"
      local part
      IFS=',' read -r -a _parts <<<"$rest"
      for part in "${_parts[@]}"; do
        part="${part#"${part%%[![:space:]]*}"}"
        part="${part%"${part##*[![:space:]]}"}"
        [[ -n "$part" ]] && resolve_one_grant "$part" "$home"
      done
      continue
    fi
    [[ -n "$val" ]] && resolve_one_grant "$val" "$home"
  done <"$f"
}

resolve_one_grant() {
  local tok="$1" home="$2" var sub base rest
  if [[ -z "$tok" || "$tok" == *@@* ]]; then
    echo "ERROR: $SCRIPT_DIR/landlock.config grant '$tok' is empty or an unsmudged @@ token" >&2
    exit 1
  fi
  if [[ "$tok" == env:HOME/* ]]; then
    tok="$home/${tok#env:HOME/}"
  elif [[ "$tok" == env:* ]]; then
    rest="${tok#env:}"
    var="${rest%%/*}"
    if [[ "$rest" == */* ]]; then
      sub="${rest#*/}"
    else
      sub=""
    fi
    base="${!var:-}"
    if [[ -z "$base" ]]; then
      echo "ERROR: landlock grant '$tok' needs \$$var, which is unset" >&2
      exit 1
    fi
    if [[ -n "$sub" ]]; then
      tok="$base/$sub"
    else
      tok="$base"
    fi
  elif [[ "$tok" != /* ]]; then
    tok="$SCRIPT_DIR/$tok"
  fi
  append_grant_dir "$tok"
}

git_common_grant() {
  local p
  if ! p="$(git -c safe.directory="$SCRIPT_DIR" -C "$SCRIPT_DIR" rev-parse --git-common-dir)"; then
    echo "ERROR: git rev-parse --git-common-dir failed in $SCRIPT_DIR" >&2
    return 1
  fi
  [[ -z "$p" ]] && return 0
  if [[ "$p" != /* ]]; then
    p="$SCRIPT_DIR/$p"
  fi
  append_grant_dir "$p"
}

libpin_host_git_grants() {
  local pin name hostgit
  for pin in "$SCRIPT_DIR"/third_party/*/libpin.toml; do
    [[ -f "$pin" ]] || continue
    name="$(basename "$(dirname "$pin")")"
    hostgit="$GIT_HOME/$name/.git"
    append_grant_dir "$hostgit"
  done
}

build_role_grant_dirs() {
  local run_user="$1" home uid
  home="$(role_home_of "$run_user")"
  SANDBOX_GRANT_DIRS=()
  # Directory stand-in for the device-node file grants. See the comment above.
  append_grant_dir /dev
  uid="$(id -u "$run_user")"
  append_grant_dir "/run/user/$uid"
  case "$ROLE_KEY" in
    planner)
      if [[ ! -d "$SCRIPT_DIR/work-history" || ! -L "$SCRIPT_DIR/TODO.md" || ! -L "$SCRIPT_DIR/ENGINEERING_LOG.md" ]]; then
        echo "ERROR: planner sandbox needs work-history/TODO.md and work-history/ENGINEERING_LOG.md, with symlinks at the old paths." >&2
        echo "  A directory grant cannot name those two files at the worktree root." >&2
        echo "  Run: sudo $SCRIPT_DIR/move-work-history.sh" >&2
        exit 1
      fi
      append_grant_dir "$SANDBOX_DIR"
      append_grant_dir "$SCRIPT_DIR/work-history"
      ;;
    coder|master)
      append_grant_dir "$SCRIPT_DIR"
      git_common_grant
      libpin_host_git_grants
      load_landlock_profile_dirs "$home"
      ;;
    orchestrator)
      append_grant_dir "$SCRIPT_DIR"
      load_landlock_profile_dirs "$home"
      append_grant_dir "${ORCH_ROOT:-}/.gradle-shared"
      append_grant_dir "${ORCH_ROOT:-}/.android-shared"
      ;;
    primary)
      append_grant_dir "$GIT_HOME"
      load_landlock_profile_dirs "$home"
      append_grant_dir "$SCRIPT_DIR/.android-shared"
      append_grant_dir "$home/.android"
      append_grant_dir "$home/.gradle"
      ;;
    *)
      echo "ERROR: no Grok sandbox profile for ROLE_KEY=$ROLE_KEY" >&2
      exit 1
      ;;
  esac
  if [[ ${#SANDBOX_GRANT_DIRS[@]} -eq 0 ]]; then
    echo "ERROR: sandbox profile for $ROLE_KEY has no writable directory" >&2
    exit 1
  fi
}

# Writes .grok/sandbox.toml (gitignored) and sets GROK_SANDBOX_PROFILE.
# Live launches keep their profiles; a dead pid's profile is dropped.
build_grok_sandbox_profile() {
  local run_user="$1"
  build_role_grant_dirs "$run_user"
  local profile="orch-launch-${ROLE_KEY}-$$"
  local dest="$SCRIPT_DIR/.grok/sandbox.toml"
  local lock="$SCRIPT_DIR/.grok/sandbox.toml.lock"
  mkdir -p "$SCRIPT_DIR/.grok"
  GROK_SANDBOX_PROFILE="$profile"
  # The heredoc is Python's stdin, so the directory list travels in the environment.
  local py_dirs="" d
  for d in "${SANDBOX_GRANT_DIRS[@]}"; do
    py_dirs+="$d"$'\n'
  done
  SANDBOX_GRANT_PATHS="$py_dirs" python3 - "$profile" "$dest" "$lock" <<'PY'
import fcntl, os, re, sys

profile, dest, lock = sys.argv[1], sys.argv[2], sys.argv[3]
dirs = []
seen = set()
for line in os.environ.get("SANDBOX_GRANT_PATHS", "").split("\n"):
    p = line.strip()
    if not p or p in seen:
        continue
    seen.add(p)
    dirs.append(p)
if not dirs:
    raise SystemExit("sandbox profile writer: no directories")

def quote(s):
    return '"' + s.replace("\\", "\\\\").replace('"', '\\"') + '"'

def section_text(name, paths):
    lines = ["[profiles.%s]" % name, 'extends = "read-only"', "restrict_network = false", "read_write = ["]
    for p in paths:
        lines.append("  %s," % quote(p))
    lines.append("]")
    return "\n".join(lines) + "\n"

mine = section_text(profile, dirs)
header = (
    "# Generated by .grok/lib/grok-launch-common.sh at launch. Do not edit.\n"
    "# Custom profile: extends read-only, child network allowed, directory grants only.\n"
    "# A same-named profile in the role user's ~/.grok/sandbox.toml would override this file.\n"
)

def parse(text):
    sections = []
    name = None
    buf = []
    for line in text.splitlines(True):
        m = re.match(r"\[profiles\.([A-Za-z0-9_-]+)\]\s*$", line)
        if m:
            if name is not None:
                sections.append((name, "".join(buf)))
            name = m.group(1)
            buf = [line]
        elif name is not None:
            buf.append(line)
    if name is not None:
        sections.append((name, "".join(buf)))
    return sections

def live(name):
    m = re.search(r"-(\d+)$", name)
    if not m:
        return False
    return os.path.isdir("/proc/%s" % m.group(1))

os.makedirs(os.path.dirname(dest), exist_ok=True)
fd = os.open(lock, os.O_CREAT | os.O_RDWR, 0o644)
try:
    fcntl.flock(fd, fcntl.LOCK_EX)
    old = ""
    if os.path.isfile(dest):
        with open(dest, "r", encoding="utf-8", errors="replace") as f:
            old = f.read()
    kept = []
    for name, body in parse(old):
        if name == profile:
            continue
        if name.startswith("orch-launch-") and live(name):
            kept.append(body if body.endswith("\n") else body + "\n")
    kept.append(mine)
    data = header + "\n" + "\n".join(kept)
    tmp = dest + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        f.write(data)
        f.flush()
        os.fsync(f.fileno())
    os.chmod(tmp, 0o644)
    os.replace(tmp, dest)
    os.chmod(dest, 0o644)
finally:
    os.close(fd)
PY
  echo "Grok sandbox: $GROK_SANDBOX_PROFILE"
  echo "  extends=read-only restrict_network=false"
  echo "  read_write:"
  for d in "${SANDBOX_GRANT_DIRS[@]}"; do
    echo "    $d"
  done
}

launch_grok_with_prompt() {
  local prompt="$1"
  local run_user
  run_user="$(resolve_run_user)"
  GROK_BIN="$(need_cfg grok_bin_default)"
  GROK_BIN="$(_expand_path "$GROK_BIN")"
  build_model_args
  build_todo_gate_flags

  if [[ ! -x "$GROK_BIN" && ! -f "$GROK_BIN" ]]; then
    echo "ERROR: grok binary not found or not executable: $GROK_BIN" >&2
    echo "Set grok_bin_default in project.config" >&2
    exit 1
  fi

  echo "Launching Grok role=$ROLE_KEY root=$SCRIPT_DIR"
  echo "Grok binary: $GROK_BIN"
  echo "Sandbox: $SANDBOX_DIR"
  echo "Running as: $run_user"
  if [[ -n "${PROMPT_FILE:-}" ]]; then
    echo "Prompt file: $PROMPT_FILE"
  fi
  if [[ -n "$ANDROID_SHARED" ]]; then
    echo "ANDROID_USER_HOME: $ANDROID_SHARED"
  fi
  echo "Tip: Ctrl+M or /multiline for multi-line input."

  # Optional free-form damage limits (primary/orch; any role if env set).
  # GROK_SANDBOX=workspace|strict|read-only|off  → pass --sandbox to grok (Grok 1.0 OS sandbox).
  # GROK_WORKTREE=1|true|name  → pass --worktree[=name] (interactive only; headless -p ignores).
  # Also pass any args after "--" on the launcher (EXTRA_ARGS).
  # Does NOT enable native plan mode or personas.
  #
  # Grok 4.6 / Build 1.0: workflows and subagents default ON in the product.
  # Planner: force both off unless already set (GROK_SUBAGENTS=1 / GROK_WORKFLOWS=1 to override).
  # Coder: force workflows off only (subagents allowed after approved execute).
  # Orch / primary / master: leave unset.
  # sudo -u env does not inherit the parent — must pass these on the env line.
  case "$ROLE_KEY" in
    planner)
      : "${GROK_SUBAGENTS:=0}"
      : "${GROK_WORKFLOWS:=0}"
      ;;
    coder)
      : "${GROK_WORKFLOWS:=0}"
      ;;
  esac
  local grok_role_env=()
  if [[ -n "${GROK_SUBAGENTS:-}" ]]; then
    grok_role_env+=(GROK_SUBAGENTS="${GROK_SUBAGENTS}")
    echo "GROK_SUBAGENTS=${GROK_SUBAGENTS}"
  fi
  if [[ -n "${GROK_WORKFLOWS:-}" ]]; then
    grok_role_env+=(GROK_WORKFLOWS="${GROK_WORKFLOWS}")
    echo "GROK_WORKFLOWS=${GROK_WORKFLOWS}"
  fi
  # The sandbox is the write boundary. Always-approve skips permission prompts.
  # Hooks and project deny rules still apply. GROK_PERMISSION_MODE=default
  # restores the ask mode. bypassPermissions is the same switch as --always-approve.
  local grok_permission_mode perm_args=()
  grok_permission_mode="${GROK_PERMISSION_MODE:-always-approve}"
  case "$grok_permission_mode" in
    always-approve|bypassPermissions|yolo)
      perm_args=(--always-approve)
      ;;
    *)
      perm_args=(--permission-mode "$grok_permission_mode")
      ;;
  esac
  echo "GROK_PERMISSION_MODE=${grok_permission_mode}"

  # User-scope [models] default_reasoning_effort (xhigh on this host) must not
  # override VE-wins “coder/master --effort high”. CLI wins.
  # GROK_REASONING_EFFORT overrides; empty/0/off skips the flag.
  # Planner/orch/primary: do not pass --effort (planner may keep xhigh).
  # Spawned execute children inherit this parent session (no spawn effort arg).
  local grok_effort_args=()
  local grok_effort="${GROK_REASONING_EFFORT:-$(role_cfg effort)}"
  if [[ -n "$grok_effort" && "$grok_effort" != "0" && "$grok_effort" != "off" ]]; then
    grok_effort_args+=(--effort "$grok_effort")
    echo "GROK_REASONING_EFFORT=${grok_effort}"
  fi
  local role_context
  role_context="$(role_cfg context)"
  if [[ -n "$role_context" ]]; then
    echo "Context window: run '/context-window ${role_context}' once in the session (roles.config; no CLI flag)."
  fi
  echo "Role card (--rules): $ROLE_CARD"
  # Profiles that extend read-only cannot create a Grok worktree. Feature
  # worktrees are created by setup_agent.sh, not by grok -w.
  if [[ -n "${GROK_WORKTREE:-}" && "${GROK_WORKTREE}" != "0" && "${GROK_WORKTREE}" != "false" ]]; then
    echo "ERROR: GROK_WORKTREE cannot be used with the role sandbox (it extends read-only)." >&2
    exit 1
  fi
  local extra
  for extra in ${EXTRA_ARGS[@]+"${EXTRA_ARGS[@]}"}; do
    if [[ "$extra" == "--sandbox" || "$extra" == --sandbox=* ]]; then
      echo "ERROR: the launcher sets --sandbox; do not pass another one." >&2
      exit 1
    fi
  done

  build_grok_sandbox_profile "$run_user"
  local exec_script
  exec_script="$(role_exec_bash "$LAUNCH_UMASK")"

  local mem_max
  mem_max="$(grok_memory_max)"
  # cd so relative sandbox paths and project hooks use this worktree.
  # shellcheck disable=SC2086
  start_role_process "$mem_max" sudo -u "$run_user" -- env \
    ${ANDROID_SHARED:+ANDROID_USER_HOME="$ANDROID_SHARED"} \
    ${ORCH_ROOT:+ORCH_ROOT="$ORCH_ROOT"} \
    ${GRADLE_USER_HOME:+GRADLE_USER_HOME="$GRADLE_USER_HOME"} \
    GROK_PROMPT_ROOT="$SCRIPT_DIR" \
    ROLE_KEY="$ROLE_KEY" \
    CHANNEL_AGENT=grok \
    GIT_HOME="$GIT_HOME" \
    ${grok_role_env[@]+"${grok_role_env[@]}"} \
    bash -c "cd \"\$1\" || exit 1; shift; ${exec_script}" bash \
      "$SCRIPT_DIR" \
      "$GROK_BIN" \
      "$prompt" \
      --cwd "$SCRIPT_DIR" \
      --rules "$(cat "$ROLE_CARD")" \
      ${MODEL_ARGS[@]+"${MODEL_ARGS[@]}"} \
      ${TODO_GATE_FLAGS[@]+"${TODO_GATE_FLAGS[@]}"} \
      --no-alt-screen \
      --minimal \
      ${perm_args[@]+"${perm_args[@]}"} \
      --sandbox "$GROK_SANDBOX_PROFILE" \
      ${grok_effort_args[@]+"${grok_effort_args[@]}"} \
      ${EXTRA_ARGS[@]+"${EXTRA_ARGS[@]}"}
}

grok_launch_main() {
  prepare_prompt "$@"
  launch_grok_with_prompt "$PROMPT"
}

# ---- cursor-agent ---------------------------------------------------------
# Same users, Landlock, env and roles.config as grok. Differences:
#   model   roles.config cursor_model= (CURSOR_FORCE_MODEL / FORCE_MODEL override)
#   card    injected by the sessionStart hook (.cursor/hooks.json) from ROLE_KEY
#   flags   --workspace <worktree> --trust --sandbox disabled (agent-landlock is the boundary)
launch_cursor_with_prompt() {
  local prompt="$1"
  local run_user
  run_user="$(resolve_run_user)"
  CURSOR_BIN="$(need_cfg cursor_bin)"
  CURSOR_BIN="$(_expand_path "$CURSOR_BIN")"
  if [[ ! -x "$CURSOR_BIN" ]]; then
    echo "ERROR: cursor-agent not found or not executable: $CURSOR_BIN" >&2
    echo "Set cursor_bin in project.config" >&2
    exit 1
  fi
  local model="${CURSOR_FORCE_MODEL:-${FORCE_MODEL:-$(role_cfg cursor_model)}}"
  local model_args=()
  [[ -n "$model" ]] && model_args=(--model "$model")
  echo "Launching cursor-agent role=$ROLE_KEY root=$SCRIPT_DIR"
  echo "cursor-agent binary: $CURSOR_BIN"
  echo "Sandbox: $SANDBOX_DIR"
  echo "Running as: $run_user"
  echo "Model: ${model:-<cursor default>}"
  echo "Role card (sessionStart hook): $ROLE_CARD"
  echo "First run as $run_user: sudo -u $run_user -H env NO_OPEN_BROWSER=1 $CURSOR_BIN login"
  # Fresh start: hook state is per conversation (cursor-hooks.sh). Remove the shared,
  # pre-session channel-armed file older hooks left, so it can never reach a new session.
  sudo -u "$run_user" -- bash -c 'f="/tmp/cursor-rules-$(id -u)/channel-armed"; [[ ! -e "$f" ]] || rm -- "$f"'
  build_landlock_args "$run_user"
  local mem_max
  mem_max="$(cursor_memory_max)"
  # shellcheck disable=SC2086
  start_role_process "$mem_max" sudo -u "$run_user" -- env \
    ${ANDROID_SHARED:+ANDROID_USER_HOME="$ANDROID_SHARED"} \
    ${ORCH_ROOT:+ORCH_ROOT="$ORCH_ROOT"} \
    ${GRADLE_USER_HOME:+GRADLE_USER_HOME="$GRADLE_USER_HOME"} \
    GROK_PROMPT_ROOT="$SCRIPT_DIR" \
    ROLE_KEY="$ROLE_KEY" \
    CHANNEL_AGENT=cursor \
    GIT_HOME="$GIT_HOME" \
    bash -c "$(role_exec_bash "$LAUNCH_UMASK")" bash \
      ${LANDLOCK_ARGS[@]+"${LANDLOCK_ARGS[@]}"} \
      "$CURSOR_BIN" \
      --workspace "$SCRIPT_DIR" \
      --trust \
      --sandbox disabled \
      ${model_args[@]+"${model_args[@]}"} \
      ${EXTRA_ARGS[@]+"${EXTRA_ARGS[@]}"} \
      "$prompt"
}

cursor_launch_main() {
  prepare_prompt "$@"
  launch_cursor_with_prompt "$PROMPT"
}
