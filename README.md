# fakeimap

Cloned from orchestration-example. Multi-agent orchestration host: launchers,
policies, Landlock helper, group-refresh shell, setup/update/fix tooling.

## Layout

- `/home/dlang/git/fakeimap/` — branch `orchestration` (managing tree)
- `/home/dlang/git/fakeimap/master/` — worktree on branch `master`
- `/home/dlang/git/fakeimap/sandbox/` — plans, research, PRs, failure logs

## Quick start

```bash
source ./orch/env
./setup_agent.sh my-feature
./run-grok-planner
```

Landlock (mutation-only) wraps agents via `agent-landlock` from `run-grok*`.
