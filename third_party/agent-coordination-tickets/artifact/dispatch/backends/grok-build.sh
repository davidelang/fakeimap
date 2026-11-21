# shellcheck shell=bash
# Backend: Grok Build headless (grok 1.0.92 flags).
# In:  B_PROMPT_FILE B_RULES_FILE B_WORKTREE B_MODEL B_EFFORT B_MAX_TURNS
#      B_ACCESS B_AGENT_BIN B_JOB_DIR B_ROLE
# Out: BACKEND_ARGV
# A fresh session every run (never --resume). The context window has no CLI
# flag: the launch hook sets it (for example GROK_HOME pointing at a
# directory whose config.toml has [model."grok-4.7"] context_window = 500000).
backend_argv() {
  BACKEND_ARGV=(
    "${B_AGENT_BIN:-grok}"
    --prompt-file "$B_PROMPT_FILE"
    --rules "$(cat "$B_RULES_FILE")"
    --output-format json
    --cwd "$B_WORKTREE"
    --always-approve
    --permission-mode auto
    --no-subagents
    --no-alt-screen
    --max-turns "${B_MAX_TURNS:-80}"
  )
  [[ -n "${B_MODEL:-}" ]] && BACKEND_ARGV+=(--model "$B_MODEL")
  [[ -n "${B_EFFORT:-}" ]] && BACKEND_ARGV+=(--reasoning-effort "$B_EFFORT")
  return 0
}

# final assistant text from the --output-format json result
backend_text() { jq -er '.text // empty' "$1"; }
backend_cost() { jq -er '.total_cost_usd // empty' "$1"; }
