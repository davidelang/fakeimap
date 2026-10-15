# grok-launch-common.sh — shared machinery for run-grok* and run-cursor* (no project law).
# Install at: <repo>/.grok/lib/grok-launch-common.sh
# Thin wrappers set GROK_LAUNCHER_DIR + ROLE_KEY, then source this and call
# grok_launch_main "$@" (grok) or cursor_launch_main "$@" (cursor-agent; see
# .cursor/lib/cursor-launch-common.sh).
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
  case "${ROLE_KEY:-}" in
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

if [[ -f "$SCRIPT_DIR/project.config" ]]; then
  # KEY=VALUE lines only (skip comments/blank). Keep equals — do not split on '='.
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line%%#*}"
    line="${line#"${line%%[![:space:]]*}"}"
    line="${line%"${line##*[![:space:]]}"}"
    [[ -z "$line" ]] && continue
    [[ "$line" =~ ^[a-zA-Z_][a-zA-Z0-9_]*= ]] || continue
    # shellcheck disable=SC2163
    export "$line"
  done <"$SCRIPT_DIR/project.config" || true
fi

# Prefer explicit GROK_BIN, then project.config keys (VE uses grok_bin_default).
# project.config may use $HOME/… as literal text — expand after load.
# git_home from project.config is ABSOLUTE (e.g. /home/you/git), not ~/git.
_expand_path() {
  local p="$1"
  p="${p/#\~/$HOME}"
  if [[ "$p" == *'$HOME'* ]]; then
    p="${p//\$HOME/$HOME}"
  fi
  printf '%s' "$p"
}
_raw_bin="${GROK_BIN:-${grok_bin:-${grok_bin_default:-}}}"
if [[ -z "$_raw_bin" || "$_raw_bin" == @@* ]]; then
  _raw_bin="${HOME}/git/grok/bin/grok"
fi
GROK_BIN="$(_expand_path "$_raw_bin")"
_raw_cbin="${CURSOR_BIN:-${cursor_bin:-}}"
if [[ -z "$_raw_cbin" || "$_raw_cbin" == @@* ]]; then
  _raw_cbin="${HOME}/git/cursor/bin/cursor-agent"
fi
CURSOR_BIN="$(_expand_path "$_raw_cbin")"

# Shared host-clone root for third_party / landlock (absolute path in project.config)
GIT_HOME_CONFIG="${git_home:-}"
if [[ -n "$GIT_HOME_CONFIG" && "$GIT_HOME_CONFIG" != @@* ]]; then
  export GIT_HOME="${GIT_HOME:-$GIT_HOME_CONFIG}"
fi

# Prefer sandbox_dir / sandbox_path from project.config; stamped default:
# sandbox_path=@@SANDBOX_PATH@@  (smudge from project.config; clean restores token)
_sandbox_stamp="@@SANDBOX_PATH@@"
SANDBOX_REL="${sandbox_dir:-${sandbox_path:-}}"
if [[ -z "$SANDBOX_REL" || "$SANDBOX_REL" == @@* ]]; then
  if [[ "$_sandbox_stamp" != @@* && -n "$_sandbox_stamp" ]]; then
    SANDBOX_REL="$_sandbox_stamp"
  fi
fi
if [[ -z "$SANDBOX_REL" || "$SANDBOX_REL" == @@* ]]; then
  # Layout discovery only (no machine-absolute paths)
  if [[ -d "$SCRIPT_DIR/dev-ai-interaction" || -L "$SCRIPT_DIR/dev-ai-interaction" ]]; then
    SANDBOX_REL="dev-ai-interaction"
  elif [[ -d "$SCRIPT_DIR/sandbox" || -L "$SCRIPT_DIR/sandbox" ]]; then
    SANDBOX_REL="sandbox"
  elif [[ -d "$SCRIPT_DIR/../dev-ai-interaction" || -L "$SCRIPT_DIR/../dev-ai-interaction" ]]; then
    SANDBOX_REL="../dev-ai-interaction"
  elif [[ -d "$SCRIPT_DIR/../sandbox" || -L "$SCRIPT_DIR/../sandbox" ]]; then
    SANDBOX_REL="../sandbox"
  else
    echo "ERROR: project.config sandbox_dir/sandbox_path is unset and no sandbox directory exists." >&2
    exit 1
  fi
fi
if [[ "$SANDBOX_REL" = /* ]]; then
  SANDBOX_DIR="$SANDBOX_REL"
else
  SANDBOX_DIR="$SCRIPT_DIR/$SANDBOX_REL"
fi
# Normalize .. in path
SANDBOX_DIR="$(cd "$SANDBOX_DIR" 2>/dev/null && pwd || echo "$SANDBOX_DIR")"


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

umask "${umask_launch:-002}"

PACK_PATHS=()
EXTRA_ARGS=()
MODEL_ARGS=()
TODO_GATE_FLAGS=()
PROMPT_FILE=""
PROMPT=""

resolve_run_user() {
  case "${ROLE_KEY:-}" in
    planner) echo "${planning_user:-ai-planner}" ;;
    coder) echo "${coder_user:-ai-coder}" ;;
    master) echo "${master_user:-${coder_user:-ai-coder}}" ;;
    orchestrator) echo "${orchestrator_user:-ai-orchestrator}" ;;
    primary) echo "${primary_user:-${SUDO_USER:-${USER:?primary_user is unset}}}" ;;
    *) echo "${primary_user:-${USER:?primary_user is unset}}" ;;
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
  case "${ROLE_KEY:-}" in
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
  if [[ -f "$start" ]]; then
    PROMPT_FILE="$start"
    PROMPT="$(cat "$start")"
  else
    PROMPT="Confirm pwd once. Then wait for a directive."
  fi
  ROLE_CARD="$SCRIPT_DIR/roles/${ROLE_KEY:-primary}.md"
  if [[ ! -f "$ROLE_CARD" ]]; then
    echo "ERROR: missing role card: $ROLE_CARD" >&2
    exit 1
  fi
}

# role_cfg KEY: value of KEY for ROLE_KEY in roles.config (blank if unset).
role_cfg() {
  local key="$1" f="$SCRIPT_DIR/roles.config" line tok
  [[ -f "$f" ]] || return 0
  line="$(grep -E "^${ROLE_KEY:-primary}[[:space:]]" "$f" | head -1)"
  for tok in $line; do
    [[ "$tok" == "$key="* ]] && { printf '%s' "${tok#*=}"; return 0; }
  done
  return 0
}

# build_landlock_args RUN_USER: sets LANDLOCK_ARGS (agent-landlock prefix) for any agent CLI.
# Mutation-only Landlock as the role user, immediately before the agent binary.
# Missing helper warns and continues. Helper itself honors AGENT_LANDLOCK_DISABLE=1.
# Owner -x is not enough: ai-planner is not in ai-code; 774 → Permission denied.
LANDLOCK_ARGS=()
build_landlock_args() {
  local run_user="$1"
  local landlock_helper="${SCRIPT_DIR}/agent-landlock"
  LANDLOCK_ARGS=()
  if [[ -f "$landlock_helper" ]]; then
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
      --role "${ROLE_KEY:-primary}"
      --worktree "$SCRIPT_DIR"
      --
    )
    echo "Landlock: $landlock_helper role=${ROLE_KEY:-primary}"
  else
    echo "Landlock: helper missing at $landlock_helper — launching without session Landlock" >&2
  fi
  return 0
}

launch_grok_with_prompt() {
  local prompt="$1"
  local run_user
  run_user="$(resolve_run_user)"
  build_model_args
  build_todo_gate_flags

  if [[ ! -x "$GROK_BIN" && ! -f "$GROK_BIN" ]]; then
    echo "ERROR: grok binary not found or not executable: $GROK_BIN" >&2
    echo "Set GROK_BIN or grok_bin / grok_bin_default in project.config" >&2
    exit 1
  fi

  echo "Launching Grok role=${ROLE_KEY:-?} root=$SCRIPT_DIR"
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
  case "${ROLE_KEY:-}" in
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
  # User-scope [ui] permission_mode (always-approve on this host) must not
  # override VE-wins “launchers stay ask”. CLI wins. GROK_PERMISSION_MODE overrides.
  local grok_permission_mode
  local grok_always_approve_args=()
  grok_permission_mode="${GROK_PERMISSION_MODE:-default}"
  echo "GROK_PERMISSION_MODE=${grok_permission_mode}"
  if [[ ${#grok_always_approve_args[@]} -gt 0 ]]; then
    echo "GROK_ALWAYS_APPROVE=1"
  fi

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
  local freeform_args=()
  case "${ROLE_KEY:-}" in
    primary|orchestrator)
      if [[ -z "${GROK_SANDBOX:-}" && "${GROK_SANDBOX_DEFAULT:-}" != "" ]]; then
        GROK_SANDBOX="${GROK_SANDBOX_DEFAULT}"
      fi
      ;;
  esac
  if [[ -n "${GROK_SANDBOX:-}" && "${GROK_SANDBOX}" != "off" && "${GROK_SANDBOX}" != "0" ]]; then
    freeform_args+=(--sandbox "${GROK_SANDBOX}")
    echo "Grok sandbox: ${GROK_SANDBOX}"
  fi
  if [[ -n "${GROK_WORKTREE:-}" && "${GROK_WORKTREE}" != "0" && "${GROK_WORKTREE}" != "false" ]]; then
    if [[ "${GROK_WORKTREE}" == "1" || "${GROK_WORKTREE}" == "true" || "${GROK_WORKTREE}" == "yes" ]]; then
      freeform_args+=(--worktree)
    else
      freeform_args+=(--worktree="${GROK_WORKTREE}")
    fi
    echo "Grok worktree: ${GROK_WORKTREE}"
  fi

  build_landlock_args "$run_user"
  local landlock_args=(${LANDLOCK_ARGS[@]+"${LANDLOCK_ARGS[@]}"})

  local mem_max
  mem_max="$(grok_memory_max)"
  # shellcheck disable=SC2086
  start_role_process "$mem_max" sudo -u "$run_user" -- env \
    ${ANDROID_SHARED:+ANDROID_USER_HOME="$ANDROID_SHARED"} \
    ${ORCH_ROOT:+ORCH_ROOT="$ORCH_ROOT"} \
    ${GRADLE_USER_HOME:+GRADLE_USER_HOME="$GRADLE_USER_HOME"} \
    GROK_PROMPT_ROOT="$SCRIPT_DIR" \
    ROLE_KEY="${ROLE_KEY:-primary}" \
    GIT_HOME="${GIT_HOME:-}" \
    ${grok_role_env[@]+"${grok_role_env[@]}"} \
    bash -c "$(role_exec_bash "${umask_launch:-002}")" bash \
      ${landlock_args[@]+"${landlock_args[@]}"} \
      "$GROK_BIN" \
      "$prompt" \
      --rules "$(cat "$ROLE_CARD")" \
      ${MODEL_ARGS[@]+"${MODEL_ARGS[@]}"} \
      ${TODO_GATE_FLAGS[@]+"${TODO_GATE_FLAGS[@]}"} \
      --no-alt-screen \
      --minimal \
      --permission-mode "${grok_permission_mode}" \
      ${grok_always_approve_args[@]+"${grok_always_approve_args[@]}"} \
      ${grok_effort_args[@]+"${grok_effort_args[@]}"} \
      ${freeform_args[@]+"${freeform_args[@]}"} \
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
  if [[ ! -x "$CURSOR_BIN" ]]; then
    echo "ERROR: cursor-agent not found or not executable: $CURSOR_BIN" >&2
    echo "Set CURSOR_BIN or cursor_bin in project.config" >&2
    exit 1
  fi
  local model="${CURSOR_FORCE_MODEL:-${FORCE_MODEL:-$(role_cfg cursor_model)}}"
  local model_args=()
  [[ -n "$model" ]] && model_args=(--model "$model")
  echo "Launching cursor-agent role=${ROLE_KEY:-?} root=$SCRIPT_DIR"
  echo "cursor-agent binary: $CURSOR_BIN"
  echo "Sandbox: $SANDBOX_DIR"
  echo "Running as: $run_user"
  echo "Model: ${model:-<cursor default>}"
  echo "Role card (sessionStart hook): $ROLE_CARD"
  echo "First run as $run_user: sudo -u $run_user -H env NO_OPEN_BROWSER=1 $CURSOR_BIN login"
  build_landlock_args "$run_user"
  local mem_max
  mem_max="$(cursor_memory_max)"
  # shellcheck disable=SC2086
  start_role_process "$mem_max" sudo -u "$run_user" -- env \
    ${ANDROID_SHARED:+ANDROID_USER_HOME="$ANDROID_SHARED"} \
    ${ORCH_ROOT:+ORCH_ROOT="$ORCH_ROOT"} \
    ${GRADLE_USER_HOME:+GRADLE_USER_HOME="$GRADLE_USER_HOME"} \
    GROK_PROMPT_ROOT="$SCRIPT_DIR" \
    ROLE_KEY="${ROLE_KEY:-primary}" \
    RULES_AGENT=cursor \
    GIT_HOME="${GIT_HOME:-}" \
    bash -c "$(role_exec_bash "${umask_launch:-002}")" bash \
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
