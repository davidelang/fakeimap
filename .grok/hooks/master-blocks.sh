#!/usr/bin/env bash
# Permission blocks for every branch except orchestration.
# deploy-orchestration class `master` publishes this file there and removes it
# from the orchestration branch. rules-hooks.sh sources it when the file is present.
# apply_master_blocks uses deny(), role, cmd, and root from the caller.

git_commit_cmd() {
  command -v python3 >/dev/null 2>&1 || hook_error "python3 not installed (git commit guard)"
  [[ -f "$root/.grok/hooks/cmd-words.py" ]] || hook_error "$root/.grok/hooks/cmd-words.py missing (git commit guard)"
  local rc=0 err
  err="$(python3 "$root/.grok/hooks/cmd-words.py" git-commit "$1" 2>&1)" || rc=$?
  [[ $rc == 2 ]] && hook_error "git commit guard: $err"
  return $rc
}

apply_master_blocks() {
  local is_git_commit=0
  [[ -n "${cmd:-}" ]] || return 0
  git_commit_cmd "$cmd"
  is_git_commit=$?
  if [[ "$role" != orchestrator && $is_git_commit == 1 ]]; then
    deny "role $role does not git add or git commit. The coder commits with ./build_app or ./commit-build (AGENTS.md). This host's Commit line is the orchestrator's."
  fi
}
