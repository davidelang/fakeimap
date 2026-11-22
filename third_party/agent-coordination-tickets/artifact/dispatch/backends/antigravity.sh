# shellcheck shell=bash
# Backend: Antigravity agy (flags from agy --help).
#   agy --dangerously-skip-permissions [--model M | --effort E] --print=<card + prompt>
# --print takes the prompt as its value (agy reads `--print X` as prompt X), so it is the
# last argument and attached with '=': a prompt that starts with '-' or a newline cannot be
# read as a flag, and nothing after it is taken for the prompt. All other flags come first;
# a launch hook that adds flags (e.g. --add-dir for a sandbox outside the worktree) inserts
# them after argv[0]. agy has no rules flag, so the role card is put in front of the prompt.
# No turn cap (the dispatcher's --timeout still applies). --dangerously-skip-permissions:
# a headless run cannot answer permission prompts. --effort takes low|medium|high|max
# and only applies without --model.
backend_argv() {
  local prompt
  prompt="$(cat "$B_RULES_FILE")"$'\n\n'"$(cat "$B_PROMPT_FILE")"
  BACKEND_ARGV=("${B_AGENT_BIN:-agy}" --dangerously-skip-permissions)
  # agy models carry their effort in the name ("Gemini 3.8 Flash (Low)") and agy refuses
  # --effort next to one ("--effort is not supported for model ..."), so with a model the
  # effort (including a retry effort) is not passed.
  if [[ -n "${B_MODEL:-}" ]]; then BACKEND_ARGV+=(--model "$B_MODEL")
  elif [[ -n "${B_EFFORT:-}" ]]; then BACKEND_ARGV+=(--effort "$B_EFFORT"); fi
  BACKEND_ARGV+=("--print=$prompt")
  return 0
}
backend_text() { cat "$1"; }
backend_cost() { return 1; }
