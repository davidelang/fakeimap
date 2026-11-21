# shellcheck shell=bash
# grok-dispatch-common.sh: shared body of run-script-grok-{coder,reviewer,tester} and
# helpers for orch/dispatch-launch-hook.
#
# The wrappers run the pinned headless dispatcher
#   third_party/agent-coordination-tickets/artifact/dispatch/dispatch
# with -l orch/dispatch-launch-hook. The dispatcher knows nothing about users, Landlock or
# config homes; the hook applies them per role from project.config and roles.config:
#   dispatcher role  wrapper                    user (project.config)           landlock profile
#   executor         run-script-grok-coder      executor_user, else coder_user  executor_landlock, else coder
#   reviewer         run-script-grok-reviewer   reviewer_user, else planning_user  reviewer_landlock, else planner
#   tester           run-script-grok-tester     tester_user, else coder_user    tester_landlock, else coder
# roles.config rows executor / reviewer / tester give model, effort and context.

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

# dc_role_map ROLE: DC_USER and DC_LANDLOCK for a dispatcher role.
dc_role_map() {
  case "$1" in
    executor) DC_USER="$(dc_cfg executor_user "$(dc_cfg coder_user)")"; DC_LANDLOCK="$(dc_cfg executor_landlock coder)" ;;
    reviewer) DC_USER="$(dc_cfg reviewer_user "$(dc_cfg planning_user)")"; DC_LANDLOCK="$(dc_cfg reviewer_landlock planner)" ;;
    tester)   DC_USER="$(dc_cfg tester_user "$(dc_cfg coder_user)")"; DC_LANDLOCK="$(dc_cfg tester_landlock coder)" ;;
    *) dc_die "unknown dispatcher role '$1' (executor|reviewer|tester)" ;;
  esac
  [[ -n "$DC_USER" ]] || dc_die "no user for role $1 in project.config"
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
[[ -n "${GROK_LAUNCHER_DIR:-}" && -n "${WRAPPER_ROLE:-}" ]] || { echo "set GROK_LAUNCHER_DIR and WRAPPER_ROLE in the run-script-grok-* wrapper" >&2; exit 2; }
DC_PROG="run-script-grok-$WRAPPER_ROLE"
DC_ROOT="$(cd "$GROK_LAUNCHER_DIR" && pwd)"
DC_PIN="$DC_ROOT/third_party/agent-coordination-tickets/artifact"
DC_DISPATCH="$DC_PIN/dispatch/dispatch"
DC_HOOK="$DC_ROOT/orch/dispatch-launch-hook"
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
($DC_ROOT). Agents start through orch/dispatch-launch-hook, which picks the
user, Landlock profile and Grok config home (context window) for each role.

Wrapper options:
  -d, --diag FILE   channel plan. Without it the standard plan
                    <sandbox_dir>/connect/<git-branch>/config.diag is offered: on a terminal
                    you are asked; without a terminal it is an error unless -y.
  -y, --yes         use the standard plan without asking
  -U, --as-me       start agents as the current user (tests; no sudo). Landlock still
                    applies (AGENT_LANDLOCK_DISABLE=1 turns it off).
  -h, --help        this help, then the dispatcher's

Defaults the wrapper adds (any of them may be given again to override):
  -l orch/dispatch-launch-hook  -c <pin>/artifact/channel  -C <this worktree>
U
  [[ "$WRAPPER_ROLE" == coder ]] && echo "  -N coder  (when neither the diag nor its sidecar sets executor.node)"
  echo
  if [[ -x "$DC_DISPATCH" ]]; then echo "Dispatcher options:"; "$DC_DISPATCH" --help; fi
}

dispatch_wrapper_main() {
  local parsed diag="" yes=0 plan=0 def branch ans
  local -a fwd=() defaults=()
  parsed=$(getopt -n "$DC_PROG" -o d:yUr:l:p:f:R:1N:C:b:m:e:t:T:u:k:w:s:c:L:nvh \
    -l diag:,yes,as-me,role:,launch:,plan:,from-step:,range:,once,node:,worktree:,backend:,model:,effort:,max-turns:,timeout:,rules:,rounds:,review-launch:,sidecar:,channel:,log-dir:,dry-run,verbose,help \
    -- "$@") || { echo "usage: $DC_PROG [-d DIAG | -y] [-U] [dispatcher options]  (-h for help)" >&2; exit 2; }
  eval set -- "$parsed"
  while true; do
    case "$1" in
      -d|--diag) diag="$2"; shift 2 ;;
      -y|--yes) yes=1; shift ;;
      -U|--as-me) export DISPATCH_HOOK_AS_ME=1; shift ;;
      -r|--role) dc_die "the role is fixed by the wrapper ($DC_ROLE); run the run-script-grok-* for another role" ;;
      -h|--help) dc_usage; exit 0 ;;
      -p|--plan) plan=1; fwd+=("$1" "$2"); shift 2 ;;
      -l|--launch|-f|--from-step|-R|--range|-N|--node|-C|--worktree|-b|--backend|-m|--model|-e|--effort|-t|--max-turns|-T|--timeout|-u|--rules|-k|--rounds|-w|--review-launch|-s|--sidecar|-c|--channel|-L|--log-dir)
        fwd+=("$1" "$2"); shift 2 ;;
      -1|--once|-n|--dry-run|-v|--verbose) fwd+=("$1"); shift ;;
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

  defaults=(-l "$DC_HOOK" -c "$DC_PIN/channel" -C "$DC_ROOT")
  if [[ "$DC_ROLE" == executor && $plan == 0 ]] \
     && ! grep -qE '^[[:space:]]*(#|//)[[:space:]]*dispatch\.executor\.node=' "$diag" \
     && ! { [[ -f "$diag.dispatch" ]] && grep -qE '^[[:space:]]*(dispatch\.)?executor\.node=' "$diag.dispatch"; }; then
    defaults+=(-N coder)
  fi
  exec "$DC_DISPATCH" -d "$diag" -r "$DC_ROLE" "${defaults[@]}" "${fwd[@]}"
}
