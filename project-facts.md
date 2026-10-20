# project-facts.md — orchestration-example

- Host: pure multi-agent orchestration template (no app)
- Sandbox: `@@SANDBOX_PATH@@/`
- git_home: set in project.config to absolute path (e.g. @@GIT_HOME@@)
- Launch: `./run-grok-*` via `.grok/lib/grok-launch-common.sh` + `agent-landlock`
- Groups: source `./env` (refresh-shell setuid)
- Clone a sibling host: `./clone-orchestration-project <new-name>` → `git_home/<name>/` (branch `orchestration`) + `master/` worktree

## Build and commit
- **Build type:** none
- Build: none (template host, no product)
- Commit: none (David commits canon)

## Review after implement
- Record HEAD before the executor starts. Each phase: `git diff` that phase's paths against that phase's observable, then commit and build green before the next phase. Before handoff, re-read the plan once.
- A plan line `Review: diff` starts one reviewer. Prompt is the plan path and `<recorded>..HEAD`. Diff that range only. Check the plan's Critical Files and Acceptance lines. Reply in chat. Do not diff `master..<branch>`.
- A deletion, a doc edit, or a pin bump omits `Review: diff`.
- A reviewer `STATUS: FAIL` names a bug: the executor fixes that bug, then the reviewer runs again on the new range.
