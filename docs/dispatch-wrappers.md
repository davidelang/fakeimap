# Dispatcher wrappers

`run-script-grok-coder`, `run-script-grok-reviewer` and `run-script-grok-tester` replace a long-lived coder session with the pinned headless dispatcher (`third_party/agent-coordination-tickets/artifact/dispatch/dispatch`, upstream `docs/dispatch.md`). The dispatcher serves one channel node and starts **one fresh agent per job**. The wrappers add only what is local to this project: which plan file, which user, which Landlock profile, which Grok config home.

| Wrapper | Dispatcher role | User (project.config) | Landlock profile | roles.config row |
|--|--|--|--|--|
| `run-script-grok-coder` | executor | `executor_user`, else `coder_user` | `executor_landlock`, else `coder` | `executor` |
| `run-script-grok-reviewer` | reviewer (read-only) | `reviewer_user`, else `planning_user` | `reviewer_landlock`, else `planner` | `reviewer` |
| `run-script-grok-tester` | tester (report-only) | `tester_user`, else `coder_user` | `tester_landlock`, else `coder` | `tester` |

The reviewer uses the planner's account and Landlock profile, not a new account. The tester uses the coder's because builds write build directories. The `*_user` / `*_landlock` keys are optional; add them to `project.config` only to change these defaults.

## Running

```sh
./run-script-grok-coder 2>&1 | tee "$T/coder.log"            # asks before using <sandbox_dir>/connect/<branch>/config.diag
./run-script-grok-coder -y                                    # use it without asking
./run-script-grok-coder -d sandbox/connect/feature-x/config.diag -1   # oldest message only
./run-script-grok-coder -d DIAG -p sandbox/plans/x-plan.md     # one plan, no channel
./run-script-grok-reviewer -d DIAG -p PLAN -R A..B             # one review, no channel
./run-script-grok-coder -d DIAG -n                             # dry run: settings and agent command lines
```

Options are GNU `getopt` (short and long). The wrapper's own: `-d/--diag`, `-y/--yes`, `-U/--as-me` (agents as the current user, no sudo; for tests), `-h/--help`. Every dispatcher option passes through (`-t/--max-turns`, `-k/--rounds`, `-N/--node`, `-s/--sidecar`, `-v`, ...); `-h` prints both. The wrapper adds `-l orch/dispatch-launch-hook -c <pin>/artifact/channel -C <worktree>` (and for the coder `-N coder` unless the diag or its sidecar sets `executor.node`); giving one again overrides it.

**Plan file.** Without `-d`, the wrapper offers the standard plan of the channel tools, `<sandbox_dir>/connect/<git-branch>/config.diag`, where `<sandbox_dir>` is project.config `sandbox_dir` resolved the way `grok-launch-common.sh` does (relative to the worktree, or absolute). On a terminal it asks `Use ...? [y/N]`; `-y` accepts without asking; with no terminal and no `-y` it exits 2. The dispatcher itself never prompts.

## The launch hook

`orch/dispatch-launch-hook` implements the dispatcher's hook contract

```
dispatch-launch-hook --role ROLE --access read-write|read-only --job-dir DIR --worktree DIR -- AGENT-ARGV...
```

It maps ROLE to the user and Landlock profile above, replaces the program name (`grok`, `cursor-agent`, `agy`) with `grok_bin_default` / `cursor_bin` / `antigravity_bin`, adds `--model` and `--reasoning-effort` from the role's `roles.config` row when the dispatcher passed none (this also covers the reviews an executor runs), and starts the agent as

```
sudo -n -u USER -- env GIT_HOME=... ROLE_KEY=PROFILE [GROK_HOME=...] bash -c 'cd WT && umask 002 && exec ...' \
  orch/agent-landlock --role PROFILE --worktree WT -- AGENT-ARGV...
```

Its notes go to stderr (the dispatcher keeps them in the run's `agent.err`); stdout is the agent's.

## Context window

`roles.config` `context=` is applied for headless runs. `256k` is Grok's default and needs nothing. For any other value (`500k`) the hook runs `orch/grok-context-home` as the role's user, which makes or refreshes

```
<sandbox_dir>/grok-home/<user>/ctx-500000/
  config.toml        the user's ~/.grok/config.toml plus  [model."grok-4.7"] context_window = 500000
  everything else    symlinks to ~/.grok/... (sign-in, sessions, settings stay there)
```

and starts grok with `GROK_HOME` set to it. This is the only route that works headless: grok has no CLI flag, `/context-window` is just prompt text in `--single` mode, and the `GROK_CONFIG` overlay ignores `context_window`. It was verified with grok 1.0.92 (the session recorded `contextWindowTokens: 500000`; one without it recorded 256000). Sessions still land in the user's `~/.grok/sessions`.

To give the executor 500K, change its row: `executor ... context=500k`.

## Example

`docs/dispatch-wrappers.example.dispatch` is a sidecar for `sandbox/connect/<workspace>/config.diag`: copy it to `sandbox/connect/<workspace>/config.diag.dispatch`. The diag needs `COMPLETE`, `FAILURE`, `BLOCKED` and `STOPPED` message blocks on the coder-to-planner arrow (as in the channel example).

## Not covered here

- `sudo -n -u ai-*` must work for the person running the wrappers (as it does for `run-grok-*`).
- Each role's user must have signed in to grok once, so `~/.grok` exists for the config home.
