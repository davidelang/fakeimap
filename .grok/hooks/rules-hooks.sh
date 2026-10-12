#!/usr/bin/env bash
# rules-hooks.sh <event> : project hooks for rules v3 (grok). Reads hook JSON on stdin.
#   prompt  (UserPromptSubmit) record David's approval lines to the sandbox log.
#   pretool (PreToolUse)       ROLE_KEY write guard, approval gate, and project-facts
#                              injection (only when the file hash changed this session).
#   stop    (Stop/SubagentStop) require a final STATUS line for coder/executor/planner.
# No-op when ROLE_KEY is unset (David's own sessions). Fails open on internal errors.
# TODO-VERIFY (live): UserPromptSubmit prompt field name; ROLE_KEY visible in subagent hooks.
set -uo pipefail
ev="${1:-}"
in="$(cat)"
role="${ROLE_KEY:-}"
[[ -z "$role" || "$role" == primary ]] && exit 0
command -v jq >/dev/null 2>&1 || exit 0
j() { printf '%s' "$in" | jq -r "$1" 2>/dev/null; }
root="$(j '.workspaceRoot // .cwd // empty')"; root="${root:-$PWD}"
sid="$(j '.sessionId // empty')"; sid="${sid:-${GROK_SESSION_ID:-nosession}}"

sandbox_dir() {
  local d=""
  [[ -f "$root/project.config" ]] && d="$(sed -n 's/^sandbox_dir=//p;s/^sandbox_path=//p' "$root/project.config" | head -1)"
  [[ -z "$d" && -d "$root/dev-ai-interaction" ]] && d=dev-ai-interaction
  [[ -z "$d" ]] && d=sandbox
  [[ "$d" = /* ]] || d="$root/$d"
  printf '%s' "$d"
}
approval_log() { printf '%s/.approvals/approved.log' "$(sandbox_dir)"; }
sha() { sha256sum "$1" 2>/dev/null | awk '{print $1}'; }

case "$ev" in
prompt)
  p="$(j '.prompt // .userPrompt // .text // .input // empty')"
  [[ -z "$p" ]] && exit 0
  path="" kind=""
  if [[ "$p" =~ (^|[[:space:]])implement[[:space:]]+\`?([^[:space:]\`]+\.md) ]]; then path="${BASH_REMATCH[2]}"; kind=implement
  elif [[ "$p" =~ (^|[[:space:]])\`?([^[:space:]\`]+\.md)\`?[[:space:]]+planned([[:space:][:punct:]]|$) ]]; then path="${BASH_REMATCH[2]}"; kind=planned
  fi
  [[ -z "$path" ]] && exit 0
  [[ "$path" = /* ]] || path="$root/$path"
  log="$(approval_log)"; mkdir -p "$(dirname "$log")" 2>/dev/null || exit 0
  printf '%s %s %s %s %s\n' "$(date -Is)" "$sid" "$kind" "$(sha "$path")" "$path" >>"$log" 2>/dev/null
  exit 0 ;;
pretool)
  tool="$(j '.toolName // empty')"
  fpath="$(j '.toolInput.file_path // .toolInput.path // .toolInput.target_file // empty')"
  cmd="$(j '.toolInput.command // empty')"
  is_edit=0; case "$tool" in search_replace|write|write_file|Edit|Write|MultiEdit) is_edit=1 ;; esac
  is_build=0; [[ "$cmd" =~ (^|[[:space:];&|])\./build_app ]] && is_build=1
  deny() { jq -cn --arg r "$1" '{decision:"deny",reason:$r}'; exit 0; }
  sb="$(sandbox_dir)"
  case "$role" in
  planner|reviewer|tester)
    if [[ $is_edit == 1 && -n "$fpath" ]]; then
      [[ "$fpath" = /* ]] || fpath="$root/$fpath"
      case "$fpath" in "$sb"/*|*/project-facts.md|*/TODO.md) ;; *) deny "role $role writes only the sandbox ($sb), project-facts.md, TODO.md (AGENTS.md 'Who writes')" ;; esac
    fi
    [[ $is_build == 1 ]] && deny "role $role does not build (AGENTS.md)" ;;
  coder|executor|master)
    if [[ $is_edit == 1 || $is_build == 1 ]]; then
      log="$(approval_log)"; ok=0
      if [[ -f "$log" ]]; then
        now="$(date +%s)"
        while read -r ts _sid kind h p; do
          [[ "$kind" == implement || "$kind" == planned ]] || continue
          t="$(date -d "$ts" +%s 2>/dev/null || echo 0)"
          (( now - t < ${RULES_APPROVAL_MAX_AGE:-86400} )) || continue
          [[ -f "$p" && "$(sha "$p")" == "$h" ]] && { ok=1; break; }
        done < <(tail -n 50 "$log")
      fi
      [[ $ok == 1 ]] || deny "no recorded approval: David must say 'implement <plan path>' (or '<roadmap> planned'), and the plan must be unchanged since then"
    fi ;;
  esac
  # project-facts injection, only when changed since last injected in this session
  pf="$root/project-facts.md"
  if [[ -f "$pf" ]]; then
    st="${TMPDIR:-/tmp}/grok-facts-$(id -u)"; mkdir -p "$st" 2>/dev/null && chmod 700 "$st" 2>/dev/null
    h="$(sha "$pf")"; f="$st/${sid//\//_}"
    if [[ "$(cat "$f" 2>/dev/null)" != "$h" ]]; then
      printf '%s' "$h" >"$f" 2>/dev/null
      jq -cn --rawfile c "$pf" '{hookSpecificOutput:{hookEventName:"PreToolUse",additionalContext:("project-facts.md (changed or first use this session):\n"+$c)}}'
    fi
  fi
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
