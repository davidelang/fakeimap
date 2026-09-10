# grok-launch-common.sh — shared machinery for run-grok* (no project law).
# Install at: <repo>/.grok/lib/grok-launch-common.sh
# Thin wrappers set GROK_LAUNCHER_DIR + ROLE_KEY, then source this and call grok_launch_main "$@".
#
# Rules: only in files listed by .grok/prompts/packs/<ROLE_KEY>.pack
# Wiring: project.config (users, sandbox_dir, optional *_model)
#
# shellcheck shell=bash

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
    SANDBOX_REL="sandbox"
  fi
fi
if [[ "$SANDBOX_REL" = /* ]]; then
  SANDBOX_DIR="$SANDBOX_REL"
else
  SANDBOX_DIR="$SCRIPT_DIR/$SANDBOX_REL"
fi
# Normalize .. in path
SANDBOX_DIR="$(cd "$SANDBOX_DIR" 2>/dev/null && pwd || echo "$SANDBOX_DIR")"

COMPOSE="${SCRIPT_DIR}/.grok/prompts/compose-session-prompt.sh"
if [[ ! -f "$COMPOSE" ]]; then
  echo "ERROR: missing $COMPOSE" >&2
  exit 1
fi
[[ -x "$COMPOSE" ]] || chmod a+x "$COMPOSE" 2>/dev/null || true

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

resolve_pack_file() {
  if [[ -n "${PACK_FILE:-}" ]]; then
    if [[ "$PACK_FILE" = /* ]]; then echo "$PACK_FILE"; else echo "$SCRIPT_DIR/$PACK_FILE"; fi
    return
  fi
  if [[ -z "${ROLE_KEY:-}" ]]; then
    echo "ERROR: ROLE_KEY or PACK_FILE required" >&2
    exit 2
  fi
  local f="$SCRIPT_DIR/.grok/prompts/packs/${ROLE_KEY}.pack"
  if [[ ! -f "$f" ]]; then
    echo "ERROR: missing pack list: $f" >&2
    echo "Add one repo-relative prompt path per line (project rules live in those files)." >&2
    exit 1
  fi
  echo "$f"
}

read_pack_paths() {
  local packf="$1" line
  PACK_PATHS=()
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line%%#*}"
    line="${line#"${line%%[![:space:]]*}"}"
    line="${line%"${line##*[![:space:]]}"}"
    [[ -z "$line" ]] && continue
    PACK_PATHS+=("$line")
  done <"$packf"
  if [[ ${#PACK_PATHS[@]} -eq 0 ]]; then
    echo "ERROR: pack file empty: $packf" >&2
    exit 1
  fi
}

compose_prompt() {
  export GROK_PROMPT_ROOT="$SCRIPT_DIR"
  "$COMPOSE" "${PACK_PATHS[@]}"
}

resolve_run_user() {
  case "${ROLE_KEY:-}" in
    planner) echo "${planning_user:-ai-planner}" ;;
    coder) echo "${coder_user:-ai-coder}" ;;
    master) echo "${master_user:-${coder_user:-ai-coder}}" ;;
    orchestrator) echo "${orchestrator_user:-ai-orchestrator}" ;;
    primary) echo "${primary_user:-${SUDO_USER:-${USER:-dlang}}}" ;;
    *) echo "${primary_user:-${USER:-dlang}}" ;;
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

# Build PROMPT from pack files and/or optional prompt file argument.
prepare_prompt() {
  PROMPT_FILE=""
  PACK_PATHS=()

  # Explicit prompt file as first argument (e.g. master-written planner prompt)
  if [[ -n "${1:-}" && -f "${1:-}" ]]; then
    PROMPT_FILE="$1"
    shift
    PROMPT="$(cat "$PROMPT_FILE")"
    collect_extra_args "$@"
    return
  fi
  collect_extra_args "$@"

  local packf default_prompt
  packf="$(resolve_pack_file)"
  read_pack_paths "$packf"

  default_prompt="${SANDBOX_DIR}/.planning-agent-prompt.txt"
  if [[ "${ROLE_KEY:-}" = "planner" \
     && -f "$default_prompt" \
     && "${GROK_IGNORE_PLANNING_PROMPT_FILE:-0}" != "1" ]]; then
    PROMPT_FILE="$default_prompt"
    PROMPT="$(cat "$PROMPT_FILE")"
    return
  fi

  PROMPT="$(compose_prompt)"

  if [[ "${ROLE_KEY:-}" = "planner" && "${GROK_WRITE_PLANNING_PROMPT_FILE:-1}" = "1" ]]; then
    mkdir -p "$SANDBOX_DIR"
    printf '%s\n' "$PROMPT" >"$default_prompt"
    PROMPT_FILE="$default_prompt"
  fi
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
  if [[ ${#PACK_PATHS[@]} -gt 0 ]]; then
    echo "Prompt pack: ${PACK_PATHS[*]}"
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
  local grok_permission_mode="${GROK_PERMISSION_MODE:-default}"
  echo "GROK_PERMISSION_MODE=${grok_permission_mode}"

  # User-scope [models] default_reasoning_effort (xhigh on this host) must not
  # override VE-wins “coder/master --effort high”. CLI wins.
  # GROK_REASONING_EFFORT overrides; empty/0/off skips the flag.
  # Planner/orch/primary: do not pass --effort (planner may keep xhigh).
  # Spawned execute children inherit this parent session (no spawn effort arg).
  local grok_effort_args=()
  case "${ROLE_KEY:-}" in
    coder|master)
      local grok_effort="${GROK_REASONING_EFFORT:-high}"
      if [[ -n "$grok_effort" && "$grok_effort" != "0" && "$grok_effort" != "off" ]]; then
        grok_effort_args+=(--effort "$grok_effort")
        echo "GROK_REASONING_EFFORT=${grok_effort}"
      fi
      ;;
  esac

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

  # Mutation-only Landlock (agent-landlock) as the role user, immediately before grok.
  # Missing helper warns and continues. Helper itself honors AGENT_LANDLOCK_DISABLE=1.
  # Owner -x is not enough: ai-planner is not in ai-code; 774 → Permission denied.
  local landlock_helper="${SCRIPT_DIR}/agent-landlock"
  local landlock_args=()
  if [[ -f "$landlock_helper" ]]; then
    if ! sudo -u "$run_user" -- test -x "$landlock_helper"; then
      echo "ERROR: $run_user cannot execute $landlock_helper" >&2
      echo "  $(stat -c 'mode=%a owner=%U:%G' "$landlock_helper" 2>/dev/null || ls -la "$landlock_helper")" >&2
      echo "  $run_user groups: $(id -nG "$run_user" 2>/dev/null || true)" >&2
      echo "  Need other-execute (775) when helper is :ai-code and the role is not in ai-code." >&2
      echo "  Fix: chmod a+x $landlock_helper  (or run fix-perms / update-rules on this worktree)" >&2
      exit 1
    fi
    landlock_args=(
      "$landlock_helper"
      --role "${ROLE_KEY:-primary}"
      --worktree "$SCRIPT_DIR"
      --
    )
    echo "Landlock: $landlock_helper role=${ROLE_KEY:-primary}"
  else
    echo "Landlock: helper missing at $landlock_helper — launching without session Landlock" >&2
  fi

  # shellcheck disable=SC2086
  exec sudo -u "$run_user" -- env \
    ${ANDROID_SHARED:+ANDROID_USER_HOME="$ANDROID_SHARED"} \
    ${ORCH_ROOT:+ORCH_ROOT="$ORCH_ROOT"} \
    ${GRADLE_USER_HOME:+GRADLE_USER_HOME="$GRADLE_USER_HOME"} \
    GROK_PROMPT_ROOT="$SCRIPT_DIR" \
    GIT_HOME="${GIT_HOME:-}" \
    ${grok_role_env[@]+"${grok_role_env[@]}"} \
    bash -c 'umask '"${umask_launch:-002}"'; exec "$@"' bash \
      ${landlock_args[@]+"${landlock_args[@]}"} \
      "$GROK_BIN" \
      "$prompt" \
      ${MODEL_ARGS[@]+"${MODEL_ARGS[@]}"} \
      ${TODO_GATE_FLAGS[@]+"${TODO_GATE_FLAGS[@]}"} \
      --no-alt-screen \
      --minimal \
      --permission-mode "${grok_permission_mode}" \
      ${grok_effort_args[@]+"${grok_effort_args[@]}"} \
      ${freeform_args[@]+"${freeform_args[@]}"} \
      ${EXTRA_ARGS[@]+"${EXTRA_ARGS[@]}"}
}

grok_launch_main() {
  prepare_prompt "$@"
  launch_grok_with_prompt "$PROMPT"
}
