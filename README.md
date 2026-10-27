# orchestration-example

**App-free** multi-agent orchestration host: launchers, policies, Landlock helper,
group-refresh shell, setup/update/fix tooling — **no product application**.

## Purpose

1. Golden layout for standing up a new multi-agent repo next to VehicleExpenses.
2. Drift detection: compare this tree / VE against other clones under `git_home`
   for **orchestration tooling** and **app-agnostic policies**.

## Quick start

```bash
cp project.config.example project.config
# set git_home=/absolute/path/to/git  (required)
source ./env                 # umask 002 + group refresh via ./refresh-shell
./setup_agent.sh my-feature  # creates agent-N worktree
./run-grok-planner
```

## Clone a new host

Create `git_home/<new-name>/` (branch `orchestration`) plus a `master/` worktree
from this example. Works from this tree, from `git_home`, or with `--source`.

```bash
./clone-orchestration-project <new-name>
# from git_home:
./orchestration-example/clone-orchestration-project <new-name>
# from elsewhere:
/path/to/orchestration-example/clone-orchestration-project --source /path/to/orchestration-example <new-name>
./clone-orchestration-project --dry-run <new-name>   # print plan only
```

Uses `sudo` / `sudo -u <primary_user>` when the caller cannot write `git_home`
or fix DAC. Does not create Unix users and does not write sudoers.

`./run-grok-*` from `master/` (or any worktree) needs `orch_root=` in that
worktree’s `project.config`; the clone tool seeds dest and `master/`. Repair an
existing dest without re-cloning:

```bash
./clone-orchestration-project --repair /path/to/host
```

`--repair` also refreshes dest DAC so `ai-planner` can exec `agent-landlock` (needs
other +x; planner is not in `ai-code`). A sudo password prompt may still appear:
host sudoers NOPASSWD lists the grok binary, not the landlock wrap.

Grok launchers confine the process with a custom sandbox profile (`extends=read-only`). Cursor launchers still wrap with `agent-landlock`.

## Drift report

```bash
./tools/report-orchestration-drift.sh
# or with AI assistance:
#   see docs/ORCHESTRATION_DRIFT.md and tools/prompts/orchestration-drift-agent.md
```

## Relation to VehicleExpenses

VehicleExpenses is the living SoT for orchestration **while** product work continues
there. This repo should track **app-agnostic** copies. Use the drift tool before
porting changes into library hosts (extractmail, remotetable, …).

Library hosts use unprefixed names: `env`, `refresh-shell` (VE still has `ve-*`).
