# shellcheck shell=bash
# grok-dispatch-common.sh: shared body of run-script-grok-{coder,reviewer,tester},
# run-script-antigravity-{coder,reviewer,tester} (UNTESTED: no live agy run yet) and
# helpers for orch/dispatch-launch-hook.
# A wrapper sets WRAPPER_ROLE, and for antigravity WRAPPER_BACKEND=antigravity plus
# WRAPPER_MODEL (its default, overridable by ANTIGRAVITY_<ROLE>_MODEL, as run-antigravity-*).
# The antigravity wrappers add -b antigravity -m <model>; the hook maps roles.config
# effort (xhigh -> max) and adds --add-dir <sandbox>.
#
# The wrappers run the pinned headless dispatcher
#   third_party/agent-coordination-tickets/artifact/dispatch/dispatch
# with -l orch/dispatch-launch-hook. The dispatcher knows nothing about users, Landlock or
# config homes.
#   User: every role runs as project.config coder_user. The wrapper switches to it ONCE at
#   start, the way run-grok-coder does (grok-launch-common.sh: start_role_process +
#   sudo -u <coder_user> -- env ... bash -c "$(role_exec_bash 002)"), and re-runs itself
#   there, so the dispatcher and every agent it starts run as coder_user. -U skips that.
#   Landlock (the only separation between roles), applied per run by the hook:
#     dispatcher role  wrapper                    landlock profile
#     executor         run-script-*-coder         executor_landlock, else coder
#     reviewer         run-script-*-reviewer      reviewer_landlock, else planner
#     tester           run-script-*-tester        tester_landlock, else coder
#   The executor's in-job blocking review is the reviewer role: planner profile.
# roles.config rows executor / reviewer / tester give model, effort and context.
# Nothing is configured by path: the worktree is where this wrapper lives, the sandbox is
# project.config sandbox_dir, the standard diag is <sandbox>/connect/<branch>/config.diag.
# Rules are found as run-grok-* find theirs: AGENTS.md (grok loads it from the cwd) and
# roles/<role>.md at the worktree root, which the dispatcher passes as --rules itself.
# The wrapper passes the project's run parameters as options:
#   every role  -N <node> -l orch/dispatch-launch-hook -c <pin>/channel -C <worktree>
#   coder       --done-tag builds
#               --retry-effort <roles.config executor retry_effort>
#               --failure-log-dir <sandbox>/implementation-failure-logs
# Message verbs (replies, the coder -> reviewer REVIEW arrow) are the diag's.

# ------------------------------------------------------------ shared helpers
declare -A DC_PC=()

dc_die() { echo "${DC_PROG:-dispatch}: $*" >&2; exit 2; }

# dc_load_project_config ROOT: KEY=VALUE lines of ROOT/project.config into DC_PC.
dc_load_project_config() {
  local root="$1" line
  [[ -f "$root/project.config" ]] || dc_die "$root/project.config missing (copy project.config.example and fill it)"
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line%%#*}"
    line="${line#"${line%%[![:space:]]*}"}"
    line="${line%"${line##*[![:space:]]}"}"
    [[ "$line" =~ ^[a-zA-Z_][a-zA-Z0-9_]*= ]] || continue
    DC_PC["${line%%=*}"]="${line#*=}"
  done <"$root/project.config"
}

# dc_cfg KEY [DEFAULT]: project.config value, $HOME and ~ expanded. An @@TOKEN@@ is an error.
dc_cfg() {
  local v="${DC_PC[$1]:-${2:-}}"
  [[ "$v" == @@* ]] && dc_die "$1 is still the token $v in project.config"
  v="${v/#\~/$HOME}"; v="${v//\$HOME/$HOME}"
  printf '%s' "$v"
}

# dc_sandbox_dir ROOT: the sandbox as grok-launch-common.sh finds it: project.config
# sandbox_dir only (relative to ROOT, or absolute); it must be a directory.
dc_sandbox_dir() {
  local SANDBOX_REL dir
  SANDBOX_REL="$(dc_cfg sandbox_dir)"
  [[ -n "$SANDBOX_REL" ]] || dc_die "sandbox_dir has no value in $1/project.config"
  if [[ "$SANDBOX_REL" = /* ]]; then dir="$SANDBOX_REL"; else dir="$1/$SANDBOX_REL"; fi
  [[ -d "$dir" ]] || dc_die "sandbox_dir=$SANDBOX_REL: $dir is not a directory"
  (cd "$dir" && pwd)
}

# dc_role_cfg ROOT ROW KEY: value of KEY on the ROW line of ROOT/roles.config (blank if unset).
dc_role_cfg() {
  local f="$1/roles.config" row="$2" key="$3" line tok
  [[ -f "$f" ]] || dc_die "missing $f"
  line="$(grep -E "^${row}[[:space:]]" "$f" | head -1)" || return 0
  for tok in $line; do
    [[ "$tok" == "$key="* ]] && { printf '%s' "${tok#*=}"; return 0; }
  done
  return 0
}

# dc_role_map ROLE: DC_LANDLOCK (agent-landlock profile) for a dispatcher role.
dc_role_map() {
  case "$1" in
    executor) DC_LANDLOCK="$(dc_cfg executor_landlock coder)" ;;
    reviewer) DC_LANDLOCK="$(dc_cfg reviewer_landlock planner)" ;;
    tester)   DC_LANDLOCK="$(dc_cfg tester_landlock coder)" ;;
    *) dc_die "unknown dispatcher role '$1' (executor|reviewer|tester)" ;;
  esac
}

# dc_context_tokens VALUE: 500k -> 500000, 256K -> 256000, digits unchanged, blank stays blank.
dc_context_tokens() {
  local v="${1,,}"
  case "$v" in
    "") printf '' ;;
    *k) printf '%s' "$(( ${v%k} * 1000 ))" ;;
    *[!0-9]*) dc_die "context '$1' in roles.config is not N, Nk" ;;
    *) printf '%s' "$v" ;;
  esac
}

[[ "${DISPATCH_FUNCS_ONLY:-}" == 1 ]] && return 0

# ------------------------------------------------------------------ wrapper
set -euo pipefail
[[ -n "${GROK_LAUNCHER_DIR:-}" && -n "${WRAPPER_ROLE:-}" ]] || { echo "set GROK_LAUNCHER_DIR and WRAPPER_ROLE in the run-script-* wrapper" >&2; exit 2; }
WRAPPER_BACKEND="${WRAPPER_BACKEND:-grok}"
case "$WRAPPER_BACKEND" in grok|antigravity) ;; *) echo "unknown WRAPPER_BACKEND=$WRAPPER_BACKEND" >&2; exit 2 ;; esac
DC_PROG="run-script-$WRAPPER_BACKEND-$WRAPPER_ROLE"
DC_ROOT="$(cd "$GROK_LAUNCHER_DIR" && pwd)"
DC_PIN="$DC_ROOT/third_party/agent-coordination-tickets/artifact"
DC_DISPATCH="$DC_PIN/dispatch/dispatch"
DC_HOOK="$DC_ROOT/orch/dispatch-launch-hook"
# The tag ./build_app moves to the commit it built (orch/reset-to-builds.sh resets to it).
DC_DONE_TAG=builds
# Wiring comes from project.config, read at run time as grok-launch-common.sh does.
dc_load_project_config "$DC_ROOT"
case "$WRAPPER_ROLE" in
  coder) DC_ROLE=executor ;;
  reviewer|tester) DC_ROLE="$WRAPPER_ROLE" ;;
  *) dc_die "unknown WRAPPER_ROLE=$WRAPPER_ROLE" ;;
esac

dc_usage() {
  cat <<U
usage: $DC_PROG [-d DIAG | -y] [-U] [dispatcher options]

Runs the pinned headless dispatcher as the $DC_ROLE role in this worktree
($DC_ROOT). It first switches once to project.config coder_user (sudo -u, as
run-grok-coder does), so the dispatcher and every agent run as that user. Agents
start through orch/dispatch-launch-hook, which applies the Landlock profile, Grok
config home (context window), cwd and umask for each role.

Wrapper options:
  -d, --diag FILE   channel plan. Without it the standard plan
                    <sandbox_dir>/connect/<git-branch>/config.diag is offered: on a terminal
                    you are asked; without a terminal it is an error unless -y.
  -y, --yes         use the standard plan without asking
  -U, --as-me       stay the current user (no switch to coder_user, no sudo). Landlock
                    still applies (AGENT_LANDLOCK_DISABLE=1 turns it off).
  -h, --help        this help, then the dispatcher's

Defaults the wrapper adds (any of them may be given again to override), all found
from this worktree at run time:
  -N $WRAPPER_ROLE  -l orch/dispatch-launch-hook  -c <pin>/artifact/channel  -C <this worktree>
U
  [[ "$WRAPPER_BACKEND" == antigravity ]] && echo "  -b antigravity  -m '${WRAPPER_MODEL:-}'   (UNTESTED; agy has no turn cap, effort xhigh -> max)"
  [[ "$WRAPPER_ROLE" == coder ]] && echo "  --done-tag $DC_DONE_TAG  --retry-effort <roles.config executor retry_effort>  --failure-log-dir <sandbox>/implementation-failure-logs"
  echo
  if [[ -x "$DC_DISPATCH" ]]; then echo "Dispatcher options:"; "$DC_DISPATCH" --help; fi
}

# dc_switch_user DRY ARGS...: become project.config coder_user once, then re-run this
# wrapper with ARGS. The mechanism is run-grok-coder's own: grok-launch-common.sh is sourced
# as the coder role (project.config, ORCH_ROOT, build homes, umask) and its
# start_role_process / sudo -u / env / role_exec_bash line starts the new process.
# Returns at once when already that user. DRY=1 (-n) prints the command and stays.
dc_switch_user() {
  local dry="$1" run_user mem_max
  shift
  ROLE_KEY=coder
  export ROLE_KEY
  # shellcheck source=/dev/null
  source "$DC_ROOT/.grok/lib/grok-launch-common.sh"
  run_user="$(resolve_run_user)"
  [[ "$(id -un)" == "$run_user" ]] && return 0
  DC_SWITCH_CMD=(sudo -u "$run_user" -- env
    ${ANDROID_SHARED:+ANDROID_USER_HOME="$ANDROID_SHARED"}
    ${ORCH_ROOT:+ORCH_ROOT="$ORCH_ROOT"}
    ${GRADLE_USER_HOME:+GRADLE_USER_HOME="$GRADLE_USER_HOME"}
    ${AGENT_LANDLOCK_DISABLE:+AGENT_LANDLOCK_DISABLE="$AGENT_LANDLOCK_DISABLE"}
    GIT_HOME="$GIT_HOME" ROLE_KEY=coder
    bash -c "$(role_exec_bash "$LAUNCH_UMASK")" bash
    "$DC_ROOT/$DC_PROG" "$@")
  if [[ "$dry" == 1 ]]; then
    printf '%s: dry run, would switch to %s with:\n ' "$DC_PROG" "$run_user" >&2
    printf ' %q' "${DC_SWITCH_CMD[@]}" >&2
    printf '\n%s: dry run continues as %s\n' "$DC_PROG" "$(id -un)" >&2
    return 0
  fi
  echo "$DC_PROG: switching to $run_user (sudo -u, as run-grok-coder)" >&2
  mem_max="$(grok_memory_max)"
  start_role_process "$mem_max" "${DC_SWITCH_CMD[@]}"
}

dispatch_wrapper_main() {
  local parsed diag="" yes=0 plan=0 as_me=0 dry=0 def branch ans
  local -a fwd=() defaults=() orig=("$@")
  parsed=$(getopt -n "$DC_PROG" -o d:yUr:l:p:f:R:1N:C:b:m:e:t:T:u:w:c:L:nvh \
    -l diag:,yes,as-me,role:,launch:,plan:,from-step:,range:,once,node:,worktree:,backend:,model:,effort:,max-turns:,timeout:,rules:,review-launch:,channel:,log-dir:,dry-run,verbose,help \
    -l agent-bin:,done-tag:,retry-effort:,failure-log-dir:,review-timeout: \
    -- "$@") || { echo "usage: $DC_PROG [-d DIAG | -y] [-U] [dispatcher options]  (-h for help)" >&2; exit 2; }
  eval set -- "$parsed"
  while true; do
    case "$1" in
      -d|--diag) diag="$2"; shift 2 ;;
      -y|--yes) yes=1; shift ;;
      -U|--as-me) as_me=1; shift ;;
      -r|--role) dc_die "the role is fixed by the wrapper ($DC_ROLE); run the run-script-$WRAPPER_BACKEND-* for another role" ;;
      -h|--help) dc_usage; exit 0 ;;
      -p|--plan) plan=1; fwd+=("$1" "$2"); shift 2 ;;
      -l|--launch|-f|--from-step|-R|--range|-N|--node|-C|--worktree|-b|--backend|-m|--model|-e|--effort|-t|--max-turns|-T|--timeout|-u|--rules|-w|--review-launch|-c|--channel|-L|--log-dir|--agent-bin|--done-tag|--retry-effort|--failure-log-dir|--review-timeout)
        fwd+=("$1" "$2"); shift 2 ;;
      -n|--dry-run) dry=1; fwd+=("$1"); shift ;;
      -1|--once|-v|--verbose) fwd+=("$1"); shift ;;
      --) shift; break ;;
    esac
  done
  [[ $# -eq 0 ]] || dc_die "unexpected argument: $1"
  [[ -x "$DC_DISPATCH" ]] || dc_die "pinned dispatcher missing: $DC_DISPATCH (bump the agent-coordination-tickets pin)"
  [[ -x "$DC_HOOK" ]] || dc_die "launch hook missing or not executable: $DC_HOOK"

  if [[ -z "$diag" ]]; then
    branch="$(git -C "$DC_ROOT" symbolic-ref -q --short HEAD)" || dc_die "no -d/--diag and HEAD is detached, so there is no standard plan; pass -d FILE"
    def="$(dc_sandbox_dir "$DC_ROOT")/connect/$branch/config.diag"
    [[ -f "$def" ]] || dc_die "no -d/--diag and no standard plan at $def"
    if [[ $yes == 1 ]]; then
      diag="$def"
    elif [[ -t 0 ]]; then
      read -r -p "$DC_PROG: no -d given. Use $def? [y/N] " ans
      case "${ans,,}" in y|yes) diag="$def" ;; *) echo "$DC_PROG: not using it; pass -d FILE" >&2; exit 2 ;; esac
    else
      dc_die "no -d/--diag and no terminal to ask; pass -d $def (or -y to accept it)"
    fi
  fi
  [[ -f "$diag" ]] || dc_die "diag not found: $diag"
  diag="$(readlink -f "$diag")"
  # Asked (if at all) before the switch; the re-run gets the answer as -d.
  [[ $as_me == 1 ]] || dc_switch_user "$dry" "${orig[@]}" -d "$diag"

  defaults=(-N "$WRAPPER_ROLE" -l "$DC_HOOK" -c "$DC_PIN/channel" -C "$DC_ROOT")
  if [[ "$WRAPPER_BACKEND" == antigravity ]]; then
    defaults+=(-b antigravity)
    if [[ -n "${WRAPPER_MODEL:-}" ]]; then defaults+=(-m "$WRAPPER_MODEL"); fi
  fi
  if [[ "$DC_ROLE" == executor ]]; then
    defaults+=(--done-tag "$DC_DONE_TAG"
      --failure-log-dir "$(dc_sandbox_dir "$DC_ROOT")/implementation-failure-logs")
    local re; re="$(dc_role_cfg "$DC_ROOT" executor retry_effort)"
    if [[ -n "$re" ]]; then defaults+=(--retry-effort "$re"); fi
  fi
  exec "$DC_DISPATCH" -d "$diag" -r "$DC_ROLE" "${defaults[@]}" "${fwd[@]}"
}
