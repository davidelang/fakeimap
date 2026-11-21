# shellcheck shell=bash
# Backend: cursor-agent --print. UNTESTED in the dispatcher (flags from
# cursor-agent 2026.10.01 --help). cursor-agent has no rules flag, so the
# role card is put in front of the prompt.
backend_argv() {
  local prompt
  prompt="$(cat "$B_RULES_FILE")"$'\n\n'"$(cat "$B_PROMPT_FILE")"
  BACKEND_ARGV=("${B_AGENT_BIN:-cursor-agent}" --print --output-format json --workspace "$B_WORKTREE" --trust --force)
  [[ -n "${B_MODEL:-}" ]] && BACKEND_ARGV+=(--model "$B_MODEL")
  BACKEND_ARGV+=("$prompt")
  return 0
}
backend_text() { jq -er '.result // .text // empty' "$1"; }
backend_cost() { return 1; }
