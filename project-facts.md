# project-facts.md — orchestration-example

- Host: pure multi-agent orchestration template (no app)
- Sandbox: `@@SANDBOX_PATH@@/`
- git_home: set in project.config to absolute path (e.g. @@GIT_HOME@@)
- Launch: `./run-grok-*` via `.grok/lib/grok-launch-common.sh` + `agent-landlock` (Grok and cursor). Planner writes `work-history/` (root `TODO.md` and `ENGINEERING_LOG.md` are symlinks).
- Groups: source `./env` (refresh-shell setuid)
- Clone a sibling host: `./clone-orchestration-project <new-name>` → `git_home/<name>/` (branch `orchestration`) + `master/` worktree
- Before adding or changing user-visible wording, translations, or the user manual: read `docs/facts/translations.md`.

## Build and commit
- **Build type:** none
- Build: none (template host, no product)
- Commit: `git add` the files of that change, then `git commit` as David Lang <david@lang.hm>. Do not push.

## Review after implement
- Record HEAD before the executor starts. The executor makes all the plan's changes, `git diff`s them against Acceptance, then commits and builds once. Before handoff, re-read the plan once.
- A plan line `Review: diff` starts one reviewer. Prompt is the plan path and `<recorded>..HEAD`. Diff that range only. Check it against the plan's Aim and Acceptance lines; flag unrelated changes, not files missing from Critical Files. Reply in chat. Do not diff `master..<branch>`.
- A deletion, a doc edit, or a pin bump omits `Review: diff`.
- A reviewer `STATUS: FAIL` names a bug: the executor fixes that bug, then the reviewer runs again on the new range.
