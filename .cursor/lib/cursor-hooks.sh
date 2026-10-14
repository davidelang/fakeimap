#!/usr/bin/env bash
# cursor-hooks.sh <event> : cursor-agent hooks (.cursor/hooks.json) -> rules-hooks.sh.
# Translates Cursor hook JSON (snake_case) to the grok field names rules-hooks.sh reads,
# and its answers back to Cursor's output schema.
#   session  (sessionStart)       additional_context = roles/$ROLE_KEY.md (the role card)
#   pretool  (preToolUse)         role write limits -> {"permission":...}
#   posttool (postToolUse)        project-facts.md when changed -> additional_context
#   response (afterAgentResponse) remember the last assistant text for 'stop'
#   stop     (stop)               final STATUS line, then channel inbox backstop -> followup_message
# No-op for David's own sessions (ROLE_KEY unset/primary) except the channel backstop.
# TODO-VERIFY (live): ROLE_KEY reaches hook processes; sessionStart context is applied.
set -uo pipefail
ev="${1:-}"
in="$(cat)"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
root="${GROK_PROMPT_ROOT:-${CURSOR_PROJECT_DIR:-$here}}"
rh="$root/.grok/hooks/rules-hooks.sh"
role="${ROLE_KEY:-}"
command -v jq >/dev/null 2>&1 || { [[ "$ev" == prompt ]] && echo '{"continue":true}'; [[ "$ev" == pretool ]] && echo '{"permission":"allow"}'; exit 0; }
j() { printf '%s' "$in" | jq -r "$1" 2>/dev/null; }
cid="$(j '.conversation_id // .session_id // empty')"; cid="${cid:-nosession}"
ws="$(j '.workspace_roots[0] // .cwd // empty')"; ws="${ws:-$root}"
st="${TMPDIR:-/tmp}/cursor-rules-$(id -u)"; mkdir -p "$st" 2>/dev/null && chmod 700 "$st" 2>/dev/null
grok() {  # grok <mode> <json> -> rules-hooks output
  printf '%s' "$2" | ROLE_KEY="$role" GROK_SESSION_ID="$cid" bash "$rh" "$1" 2>/dev/null
}
case "$ev" in
session)
  card="$root/roles/${role:-primary}.md"
  if [[ -n "$role" && "$role" != primary && -f "$card" ]]; then
    jq -cn --rawfile c "$card" '{additional_context:($c+"\nLaw: AGENTS.md (auto-loaded).")}'
  else echo '{}'; fi ;;
prompt)  # no longer registered; kept answering for stale hooks.json copies
  echo '{"continue":true}' ;;
pretool)
  g="$(jq -cn --argjson i "$in" --arg w "$ws" --arg s "$cid" \
     '{toolName:($i.tool_name//""),toolInput:($i.tool_input//{}),workspaceRoot:$w,sessionId:$s}' 2>/dev/null)"
  out="$(RULES_NO_FACTS=1 grok pretool "$g")"
  if [[ "$(printf '%s' "$out" | jq -r '.decision // empty' 2>/dev/null)" == deny ]]; then
    printf '%s' "$out" | jq -c '{permission:"deny",user_message:.reason,agent_message:.reason}'
  else echo '{"permission":"allow"}'; fi ;;
posttool)
  out="$(grok facts "$(jq -cn --arg w "$ws" --arg s "$cid" '{workspaceRoot:$w,sessionId:$s}')")"
  ctx="$(printf '%s' "$out" | jq -r '.hookSpecificOutput.additionalContext // empty' 2>/dev/null)"
  if [[ -n "$ctx" ]]; then jq -cn --arg c "$ctx" '{additional_context:$c}'; else echo '{}'; fi ;;
response)
  printf '%s' "$(j '.text // empty')" >"$st/last-${cid//\//_}" 2>/dev/null
  echo '{}' ;;
stop)
  [[ "$(j '.status // "completed"')" == completed ]] || { echo '{}'; exit 0; }
  last="$(cat "$st/last-${cid//\//_}" 2>/dev/null)"
  active=false; [[ "$(j '.loop_count // 0')" != 0 ]] && active=true
  out=""
  if [[ -n "$role" && "$role" != primary ]]; then
    out="$(grok stop "$(jq -cn --arg m "$last" --argjson a "$active" '{lastAssistantMessage:$m,stopHookActive:$a}')")"
  fi
  if [[ "$(printf '%s' "$out" | jq -r '.decision // empty' 2>/dev/null)" == block ]]; then
    printf '%s' "$out" | jq -c '{followup_message:.reason}'; exit 0
  fi
  # Channel backstop: only when this session armed a watch (cursor-channel-watch).
  armf="$st/channel-armed"
  if [[ -f "$armf" ]]; then
    log="$(cat "$armf")"; seen="$st/channel-seen-${cid//\//_}"
    f="$(grep '^FILE ' "$log" 2>/dev/null | tail -1)"
    if [[ -n "$f" && -f "${f#FILE }" && "$(cat "$seen" 2>/dev/null)" != "$f" ]]; then
      printf '%s' "$f" >"$seen"
      jq -cn --arg f "$f" '{followup_message:("Channel: "+$f+" (oldest inbox file). Handle it per docs/rules/channel.md.")}'
      exit 0
    fi
  fi
  echo '{}' ;;
*) echo '{}' ;;
esac
exit 0
