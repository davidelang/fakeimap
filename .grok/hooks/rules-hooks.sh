#!/usr/bin/env bash
# rules-hooks.sh <event> : project hooks for rules v3 (grok). Reads hook JSON on stdin.
#   pretool (PreToolUse)       ROLE_KEY write guard and project-facts
#                              injection (only when the file hash changed this session).
#   stop    (Stop/SubagentStop) require a final STATUS line for coder/executor/planner.
#   facts   project-facts injection only (cursor shim: postToolUse additional_context).
# RULES_NO_FACTS=1 makes pretool skip the injection (cursor runs it as 'facts').
# Cursor drives this script through .cursor/lib/cursor-hooks.sh (field translation).
# No-op when ROLE_KEY is unset or primary (David's own sessions). For role sessions a missing
# input (jq, GROK_PROMPT_ROOT, session id, project.config sandbox_dir) is an ERROR: pretool
# denies and stop blocks with the reason, so it is never silent.
# TODO-VERIFY (live): ROLE_KEY visible in subagent hooks.
# PreToolUse is failClosed in rules-hooks.json. Do not disable these hooks in /hooks:
# since grok 1.0.75 that disable is per repository and drops the write guard here.
# failClosed is ignored on Stop and SubagentStop.
set -uo pipefail
ev="${1:-}"
in="$(cat)"
role="${ROLE_KEY:-}"
[[ -z "$role" || "$role" == primary ]] && exit 0
hook_error() {  # one loud answer per event; jq may be the missing piece
  local m="rules-hooks: ERROR: $1"
  echo "$m" >&2
  m="${m//\\/\\\\}"; m="${m//\"/\\\"}"
  case "$ev" in
  pretool) printf '{"decision":"deny","reason":"%s"}\n' "$m" ;;
  stop) printf '{"decision":"block","reason":"%s"}\n' "$m" ;;
  esac
  exit 0
}
command -v jq >/dev/null 2>&1 || hook_error "jq not installed"
j() { printf '%s' "$in" | jq -r "$1" 2>/dev/null; }
root="${GROK_PROMPT_ROOT:-}"
[[ -n "$root" && -d "$root" ]] || hook_error "GROK_PROMPT_ROOT unset (start agents with the run-* launchers)"
sid="$(j '.sessionId // empty')"  # cursor-hooks.sh passes its conversation_id here
[[ -n "$sid" ]] || hook_error "hook input has no sessionId"

sandbox_dir() {  # sets sb
  local d
  [[ -f "$root/project.config" ]] || hook_error "$root/project.config missing"
  d="$(sed -n 's/^sandbox_dir=//p' "$root/project.config" | tail -1)"
  [[ -n "$d" && "$d" != @@* ]] || hook_error "sandbox_dir has no value in $root/project.config"
  [[ "$d" = /* ]] || d="$root/$d"
  sb="$d"
}
# build_cmd CMD: exit 1 when a command word of CMD is build_app/commit-build (cmd-words.py;
# arguments such as channel-send message text are never matched). Unparsable = hook ERROR.
# master-blocks.sh (class master) holds the git add/commit block. Absent on the orchestration branch only.
build_cmd() {
  command -v python3 >/dev/null 2>&1 || hook_error "python3 not installed (build guard)"
  [[ -f "$root/.grok/hooks/cmd-words.py" ]] || hook_error "$root/.grok/hooks/cmd-words.py missing (build guard)"
  local rc=0 err
  err="$(python3 "$root/.grok/hooks/cmd-words.py" build "$1" 2>&1)" || rc=$?
  [[ $rc == 2 ]] && hook_error "build guard: $err"
  return $rc
}
if [[ -f "$root/.grok/hooks/master-blocks.sh" ]]; then
  # shellcheck disable=SC1091
  source "$root/.grok/hooks/master-blocks.sh"
fi
sha() { sha256sum "$1" 2>/dev/null | awk '{print $1}'; }

inject_facts() {  # $1 = hook event name
  local pf="$root/project-facts.md" st h f
  if [[ -f "$pf" ]]; then
    sandbox_dir
    st="$sb/.grok-facts-$(id -u)"
    mkdir -p "$st" && chmod 700 "$st" || hook_error "cannot create hook state dir $st"
    h="$(sha "$pf")"; f="$st/${sid//\//_}"
    if [[ "$(cat "$f" 2>/dev/null)" != "$h" ]]; then
      printf '%s' "$h" >"$f" 2>/dev/null
      jq -cn --arg e "$1" --rawfile c "$pf" '{hookSpecificOutput:{hookEventName:$e,additionalContext:("project-facts.md (changed or first use this session):\n"+$c)}}'
    fi
  fi
}

case "$ev" in
pretool)
  tool="$(j '.toolName // empty')"
  fpath="$(j '.toolInput.file_path // .toolInput.path // .toolInput.target_file // empty')"
  cmd="$(j '.toolInput.command // empty')"
  is_edit=0; case "$tool" in search_replace|write|write_file|Edit|Write|MultiEdit|Delete|StrReplace) is_edit=1 ;; esac
  is_build=0; [[ -n "$cmd" ]] && { build_cmd "$cmd"; is_build=$?; }
  deny() { jq -cn --arg r "$1" '{decision:"deny",reason:$r}'; exit 0; }
  sandbox_dir
  if declare -F apply_master_blocks >/dev/null 2>&1; then
    apply_master_blocks
  fi
  case "$role" in
  planner|reviewer|tester)
    if [[ $is_edit == 1 && -n "$fpath" ]]; then
      [[ "$fpath" = /* ]] || fpath="$root/$fpath"
      case "$fpath" in "$sb"/*|*/project-facts.md|*/TODO.md) ;; *) deny "role $role writes only the sandbox ($sb), project-facts.md, TODO.md (AGENTS.md 'Who writes')" ;; esac
    fi
    [[ $is_build == 1 ]] && deny "role $role does not build (AGENTS.md)" ;;
  esac
  [[ "${RULES_NO_FACTS:-0}" == 1 ]] && exit 0
  inject_facts PreToolUse
  exit 0 ;;
facts)
  inject_facts PostToolUse
  exit 0 ;;
stop)
  case "$role" in coder|executor|planner) ;; *) exit 0 ;; esac
  [[ "$(j '.stopHookActive // false')" == true ]] && exit 0
  last="$(j '.lastAssistantMessage // empty' | sed -e 's/[[:space:]]*$//' | awk 'NF{l=$0} END{print l}')"
  if [[ ! "$last" =~ ^STATUS:\ (SUCCESS|FAIL|BLOCKED|NEEDS-INPUT|WORKING)([[:space:]].*)?$ ]]; then
    jq -cn '{decision:"block",reason:"End your turn with a final line: STATUS: SUCCESS|FAIL|BLOCKED|NEEDS-INPUT|WORKING <short note> (AGENTS.md)."}'
  fi
  exit 0 ;;
esac
exit 0
