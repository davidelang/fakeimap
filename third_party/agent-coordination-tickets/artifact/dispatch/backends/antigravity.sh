# shellcheck shell=bash
# Backend: Antigravity agy --print. UNTESTED in the dispatcher (flags from
# agy 1.2.12 --help). The role card is put in front of the prompt.
backend_argv() {
  local prompt
  prompt="$(cat "$B_RULES_FILE")"$'\n\n'"$(cat "$B_PROMPT_FILE")"
  BACKEND_ARGV=("${B_AGENT_BIN:-agy}" --print)
  [[ -n "${B_EFFORT:-}" ]] && BACKEND_ARGV+=(--effort "$B_EFFORT")
  [[ -n "${B_MODEL:-}" ]] && BACKEND_ARGV+=(--model "$B_MODEL")
  BACKEND_ARGV+=("$prompt")
  return 0
}
backend_text() { cat "$1"; }
backend_cost() { return 1; }
