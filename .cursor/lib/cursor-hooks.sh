#!/usr/bin/env bash
# cursor-hooks.sh <event> : cursor-agent hooks (.cursor/hooks.json) -> rules-hooks.sh.
# Translates Cursor hook JSON (snake_case) to the grok field names rules-hooks.sh reads,
# and its answers back to Cursor's output schema.
#   session  (sessionStart)       additional_context = roles/$ROLE_KEY.md (the role card)
#   pretool  (preToolUse)         role write limits -> {"permission":...}
#   posttool (postToolUse)        project-facts.md when changed -> additional_context;
#                                 records this conversation's own channel watch (channel-armed-<cid>)
#   response (afterAgentResponse) remember the last assistant text for 'stop'
#   stop     (stop)               final STATUS line, then channel inbox backstop -> followup_message
# No-op for David's own sessions (ROLE_KEY unset/primary) except the channel backstop.
# All session state under $st is keyed by conversation id: a new session (no /channel run in it)
# never reads another session's state.
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
  # Channel arming: only this conversation's own run of the wrapper (or its --stop) counts.
  if [[ "$(j '.tool_name // empty')" == Shell ]]; then
    cmd="$(j '.tool_input.command // empty')"
    armf="$st/channel-armed-${cid//\//_}"
    if [[ "$cmd" =~ (^|[[:space:];&|/])\.cursor/lib/cursor-channel-watch[[:space:]]+([^[:space:];&|]+)[[:space:]]+([^[:space:];&|]+)([[:space:]]+(--stop))? ]]; then
      cplan="${BASH_REMATCH[2]}" cnode="${BASH_REMATCH[3]}"
      if [[ -n "${BASH_REMATCH[5]}" ]]; then
        rm -f -- "$armf"
      else
        if ! top="$(git -C "$ws" rev-parse --show-toplevel 2>&1)"; then
          jq -cn --arg e "$top" '{additional_context:("CHANNEL-ARM-NOT-RECORDED: workspace is not a git checkout: "+$e)}'
          exit 0
        fi
        key="$(printf '%s %s %s' "$top" "$cplan" "$cnode" | sha256sum | cut -c1-12)"
        jq -cn --arg l "$st/channel-$key.log" --arg p "$cplan" --arg n "$cnode" --arg w "$ws" \
          '{log:$l,plan:$p,node:$n,ws:$w}' >"$armf"
      fi
    fi
  fi
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
  # Channel backstop: only for a watch this conversation armed, in this workspace.
  armf="$st/channel-armed-${cid//\//_}"
  if [[ -f "$armf" && "$(jq -r .ws "$armf")" == "$ws" ]]; then
    log="$(jq -r .log "$armf")"; seen="$st/channel-seen-${cid//\//_}"
    f="$(grep '^FILE ' "$log" 2>/dev/null | tail -1)"
    if [[ -n "$f" && -f "${f#FILE }" && "$(cat "$seen" 2>/dev/null)" != "$f" ]]; then
      printf '%s' "$f" >"$seen"
      jq -cn --arg f "$f" --arg p "$(jq -r .plan "$armf")" --arg n "$(jq -r .node "$armf")" \
        '{followup_message:("Channel (armed in this session: "+$p+" "+$n+"): "+$f+" (oldest inbox file). Handle it as the --whatineedtoknow header says.")}'
      exit 0
    fi
  fi
  echo '{}' ;;
*) echo '{}' ;;
esac
exit 0
