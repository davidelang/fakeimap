# Dispatcher wrappers

`run-script-grok-coder`, `run-script-grok-reviewer` and `run-script-grok-tester` replace a long-lived coder session with the pinned headless dispatcher (`third_party/agent-coordination-tickets/artifact/dispatch/dispatch`, upstream `docs/dispatch.md`). The dispatcher serves one channel node and starts **one fresh agent per job**. The wrappers add only what is local to this project: which plan file, which user, which Landlock profile, which Grok config home.

| Wrapper | Dispatcher role | Landlock profile | roles.config row |
|--|--|--|--|
| `run-script-grok-coder` | executor | `executor_landlock`, else `coder` | `executor` |
| `run-script-grok-reviewer` | reviewer (read-only) | `reviewer_landlock`, else `planner` | `reviewer` |
| `run-script-grok-tester` | tester (report-only) | `tester_landlock`, else `coder` | `tester` |

**Antigravity (UNTESTED: no live agy run yet).** `run-script-antigravity-coder`, `-reviewer` and `-tester` are the same wrappers (same roles, Landlock, user switch, options) with `-b antigravity` and `-m <model>`. The model is a default in each wrapper (`Gemini 3.5 Flash (High)`, as `run-antigravity-planner` / `-master`), overridden by `ANTIGRAVITY_CODER_MODEL` / `ANTIGRAVITY_REVIEWER_MODEL` / `ANTIGRAVITY_TESTER_MODEL`. The dispatcher runs `agy --print --dangerously-skip-permissions [--effort E] [--model M] "<role card>\n\n<prompt>"` (agy has no rules flag); the hook replaces `agy` with project.config `antigravity_bin` and inserts, before the prompt (agy reads flags only there), `--add-dir <sandbox_dir>` and `--effort` from the role's `roles.config` row, mapped to agy's low|medium|high|max (`xhigh` -> `max`, also for `--retry-effort`). agy has no turn cap (`-T` timeout still applies), no cost report and no context-window setting; it loads `AGENTS.md` as `GEMINI.md` itself.

**One user, Landlock per role.** Every role runs as project.config `coder_user`. Each wrapper switches to it once, at start, with run-grok-coder's own mechanism: it sources `.grok/lib/grok-launch-common.sh` as the coder role and starts itself again through its `start_role_process` + `sudo -u <coder_user> -- env ... bash -c "$(role_exec_bash 002)"` line. The dispatcher and every agent it starts then run as that user, so there is at most one sudo password prompt per script start (none with NOPASSWD or a cached sudo timestamp) and no sudo per job. Roles are kept apart only by Landlock: `coder` for the executor and tester (builds write build directories), `planner` for the reviewer, including the blocking review the executor runs inside each job. The `*_landlock` keys are optional. `-U/--as-me` skips the switch (tests). Inboxes, logs, `costs.tsv` and job directories must be writable by `coder_user`: keep the sandbox's `connect/` tree group `ai-sandbox`, setgid, group-writable (2775), as `channel-setup` makes it.

## Running

```sh
./run-script-grok-coder 2>&1 | tee "$T/coder.log"            # asks before using <sandbox_dir>/connect/<branch>/config.diag
./run-script-grok-coder -y                                    # use it without asking
./run-script-grok-coder -d sandbox/connect/feature-x/config.diag -1   # oldest message only
./run-script-grok-coder -d DIAG -p sandbox/plans/x-plan.md     # one plan, no channel
./run-script-grok-reviewer -d DIAG -p PLAN -R A..B             # one review, no channel
./run-script-grok-coder -d DIAG -n                             # dry run: settings and agent command lines
```

Options are GNU `getopt` (short and long). The wrapper's own: `-d/--diag`, `-y/--yes`, `-U/--as-me` (stay the current user: no switch to `coder_user`, no sudo; for tests), `-h/--help`. Every dispatcher option passes through (`-t/--max-turns`, `-N/--node`, `--review-timeout`, `-v`, ...); `-h` prints both. The wrapper adds `-N <role node> -l orch/dispatch-launch-hook -c <pin>/artifact/channel -C <worktree>` and, for the coder, `--done-tag builds --retry-effort <roles.config> --failure-log-dir <sandbox>/implementation-failure-logs` (see Where things are set); giving one again overrides it.

**Plan file.** Without `-d`, the wrapper offers the standard plan of the channel tools, `<sandbox_dir>/connect/<git-branch>/config.diag`, where `<sandbox_dir>` is project.config `sandbox_dir` resolved the way `grok-launch-common.sh` does (relative to the worktree, or absolute). On a terminal it asks `Use ...? [y/N]` (before the user switch; the re-run gets the answer as `-d`); `-y` accepts without asking; with no terminal and no `-y` it exits 2. The dispatcher itself never prompts. With `-n` the wrapper prints the switch command it would run and does the dry run as the current user.

## The launch hook

`orch/dispatch-launch-hook` implements the dispatcher's hook contract

```
dispatch-launch-hook --role ROLE --access read-write|read-only --job-dir DIR --worktree DIR -- AGENT-ARGV...
```

It runs as the user it is called as (already `coder_user`; no sudo). It maps ROLE to the Landlock profile above, replaces the program name (`grok`, `cursor-agent`, `agy`) with `grok_bin_default` / `cursor_bin` / `antigravity_bin`, adds `--model` and `--reasoning-effort` from the role's `roles.config` row when the dispatcher passed none (this also covers the reviews an executor runs); for `agy` it inserts `--add-dir <sandbox>` and the mapped `--effort` instead, and, in the worktree with umask 002, execs

```
env GIT_HOME=... ROLE_KEY=PROFILE DISPATCH_ROLE=... DISPATCH_ACCESS=... [GROK_HOME=...] \
  orch/agent-landlock --role PROFILE --worktree WT -- AGENT-ARGV...
```

Its notes go to stderr (the dispatcher keeps them in the run's `agent.err`); stdout is the agent's.

## Context window

`roles.config` `context=` is applied for headless runs. `256k` is Grok's default and needs nothing. For any other value (`500k`) the hook runs `orch/grok-context-home` (as the current user, i.e. `coder_user`), which makes or refreshes

```
<sandbox_dir>/grok-home/<user>/ctx-500000/
  config.toml        the user's ~/.grok/config.toml plus  [model."grok-4.7"] context_window = 500000
  everything else    symlinks to ~/.grok/... (sign-in, sessions, settings stay there)
```

and starts grok with `GROK_HOME` set to it. This is the only route that works headless: grok has no CLI flag, `/context-window` is just prompt text in `--single` mode, and the `GROK_CONFIG` overlay ignores `context_window`. It was verified with grok 1.0.92 (the session recorded `contextWindowTokens: 500000`; one without it recorded 256000). Sessions still land in the user's `~/.grok/sessions`.

To give the executor 500K, change its row: `executor ... context=500k`.

## Where things are set

No file holds a path or a setting for the dispatcher. Everything is found at run time from the worktree the wrapper lives in, the way the standard diag is:

- **Run parameters** are the wrapper's options to the dispatcher: `-N coder|reviewer|tester`, `-C <worktree>`, the hook and channel from this worktree and its pin; for the coder `--done-tag builds`, `--retry-effort` from the `roles.config` `executor` row, `--failure-log-dir <sandbox>/implementation-failure-logs` (sandbox from `project.config`). Turn caps, timeouts and the review wait are the dispatcher's defaults (80 / 40 / 60 turns, no timeout, 3600 s); give `-t`, `-T`, `--review-timeout` to change one run.
- **Rules for the agent** come the way `run-grok-*` gets them: grok loads `AGENTS.md` from the worktree root it runs in, and the role card `roles/<role>.md` at that root goes in as `--rules` (`run-grok-coder`: `roles/coder.md`; the dispatcher: `roles/executor.md`, `roles/reviewer.md` also for an in-process review, `roles/tester.md`). Nothing passes a card path.
- **Messages** are the diag's: nodes, a `verb coder reviewer REVIEW` line (the coder then sends its reviews there and waits), and the reply verbs `COMPLETE`, `FAILURE`, `BLOCKED`, `STOPPED` (coder to planner) and `REVIEW-PASS`, `REVIEW-FAIL`, `REVIEW-ERROR` (reviewer to coder), each with a message block.

A `config.diag.dispatch` file from an earlier version is ignored with a warning.

## Not covered here

- `sudo -u <coder_user>` must work for the person running the wrappers (as it does for `run-grok-coder`); it may ask for a password once per start.
- `coder_user` must have signed in to grok once, so `~/.grok` exists for the config home.

## Reviews and retries

The coder's dispatcher sends each blocking review straight to the reviewer node (the diag's `coder >-> reviewer` arrow and REVIEW / REVIEW-PASS / REVIEW-FAIL / REVIEW-ERROR verbs carrying `REF` and `ROUND`) and waits for the verdict itself, so reviews never wake the planner. Each job gets at most 3 review cycles: after a FAIL a fresh coder session fixes from the full review (`review-N.md`), then the range is reviewed again. A review without fewer problems (`PROBLEMS: N`) than the one before, or a third FAIL, sends FAILURE "re-plan needed" to the planner with `review-history.md` (copied to `<sandbox>/implementation-failure-logs`). A session that ends not done gets one retry at the `roles.config` `executor` `retry_effort`. The dispatcher never resets the tree, and `roles/executor.md` forbids resets past the `builds` tag; build slips (the 3-strikes rule in `AGENTS.md`) stay inside the coder's own session. BLOCKED is only for something outside the code that prevents progress (permissions, contradictory instructions, missing access). Each agent gets only a one-line job prompt (`Read the rules. Implement PLAN.`, `... Fix the issues in REVIEW-FILE ...`, `... Review A..B against PLAN.`); every rule it follows is in its `roles/<role>.md`, passed as its rules. The role (diag node and dispatcher role) is fixed by each wrapper, as `run-grok-coder` fixes `ROLE_KEY`; the dispatcher keeps the per-role defaults (access, turn caps) in its own code, with no settings file. Details: the pinned `docs/dispatch.md`.

**Paths across the user switch.** project.config values written with `$HOME` (`grok_bin_default`, `antigravity_bin`, `cursor_bin`, ...) mean the invoking user's home, as for `run-grok-*` / `run-antigravity-*`, which expand them before their sudo. The wrapper exports `DISPATCH_INVOKER_HOME` (its own `$HOME`) before switching, passes it through the `sudo -u` env, and `dc_cfg` (wrapper and hook) expands `$HOME` / `~` with it, so as coder_user the hook still runs `/home/<invoker>/git/...`. The wrapper also checks, as the invoker, that its backend's program is executable before switching.

**Launch refusals.** Every refusal of `orch/dispatch-launch-hook` happens before the agent starts (missing program, Landlock not usable: it test-runs `agent-landlock ... true` first) and exits 127 with the reason on stderr; the dispatcher then replies BLOCKED with that reason and does not retry.

