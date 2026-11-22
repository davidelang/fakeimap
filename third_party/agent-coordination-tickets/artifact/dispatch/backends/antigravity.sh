# shellcheck shell=bash
# Backend: Antigravity agy --print. UNTESTED (no live run; flags from agy --help).
# agy has no rules flag, so the role card is put in front of the prompt. It has no turn
# cap (the dispatcher's --timeout still applies). --dangerously-skip-permissions: a
# headless run cannot answer permission prompts. agy parses flags Go-style, so they
# must come before the prompt; a launch hook that adds flags (e.g. --add-dir for a
# sandbox outside the worktree) inserts them after argv[0]. --effort takes
# low|medium|high|max.
backend_argv() {
  local prompt
  prompt="$(cat "$B_RULES_FILE")"$'\n\n'"$(cat "$B_PROMPT_FILE")"
  BACKEND_ARGV=("${B_AGENT_BIN:-agy}" --print --dangerously-skip-permissions)
  [[ -n "${B_EFFORT:-}" ]] && BACKEND_ARGV+=(--effort "$B_EFFORT")
  [[ -n "${B_MODEL:-}" ]] && BACKEND_ARGV+=(--model "$B_MODEL")
  BACKEND_ARGV+=("$prompt")
  return 0
}
backend_text() { cat "$1"; }
backend_cost() { return 1; }
