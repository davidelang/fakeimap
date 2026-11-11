# ENGINEERING_LOG


## 2026-08-21 - clone-orchestration-project execution start

- Approved plan: sandbox/plans/clone-orchestration-project-20260821-1819-plan.md
- Work: add ./clone-orchestration-project to clone this example into git_home/<name> with orchestration + master worktrees
- First action per STANDARD BLOCK; host has no ./build_app (phase gate: bash -n + forensic read)

## 2026-08-21 - clone-orchestration-project landed (staged)

- Added ./clone-orchestration-project (discovery, clone --no-hardlinks as primary, orchestration+master worktrees, fix-perms --skip-sudoers)
- README.md + project-facts.md document the helper
- Verified: bash -n; --dry-run SomeName from this tree, git_home, and --source (same source/dest)
- git commit blocked: ai-orchestrator has no user.name/email (changes remain staged)

## 2026-08-21 - clone-seed-worktree-config execution start

- Approved plan: sandbox/plans/clone-seed-worktree-config-20260821-1840-plan.md
- Work: seed dest+master project.config via ve_seed_worktree_project_config; dest-root DAC 2775; --repair; repair torque-ford-gas-tracking
- First action per STANDARD BLOCK; host has no ./build_app

## 2026-08-21 - clone-seed-worktree-config script landed; live --repair blocked

- clone-orchestration-project now seeds dest+master project.config via ve_seed_worktree_project_config, dest-root chown 2775, and --repair
- Live --repair of torque-ford-gas-tracking failed in this session: dest is 770 dlang:dlang and sudo is blocked (Landlock no-new-privileges)
- Human: run as dlang ./clone-orchestration-project --repair /home/dlang/git/torque-ford-gas-tracking

## 2026-08-21 - clone-landlock-exec-dac execution start

- Approved plan: sandbox/plans/clone-landlock-exec-dac-20260821-1901-plan.md
- Work: fix-perms lists agent-landlock+env; clone do_fix_perms uses this tree fix-perms with explicit DEST and DEST/master; force landlock 775
- First action per STANDARD BLOCK; host has no ./build_app

## 2026-08-21 - clone-landlock-exec-dac landed

- fix-perms: agent-landlock and env in executable-scripts list (2775/775 o+x)
- clone-orchestration-project: this-tree fix-perms --skip-sudoers DEST and DEST/master; force landlock 775
- Live dest not repaired from this session (sudo/Landlock nnp). Human: --repair torque-ford-gas-tracking as dlang

## 2026-11-10 - imap session core

- Approved plan: sandbox/plans/imap-session-core-20261110-2318-plan.md
- Work: Go IMAP session core with a Cyrus 3.13-shaped personality, in-memory store, loopback tests, and a makefile build

## 2026-11-11 - one FLAGS when FETCH sets \Seen

- Approved plan: sandbox/plans/fetch-seen-flags-20261111-0037-plan.md
- A FETCH that already returns FLAGS, including ALL, FAST, and FULL, does not send a second FLAGS line when it sets \Seen

## 2026-11-11 - control socket and fault injection

- Approved plan: sandbox/plans/faults-control-20261111-0048-plan.md
- Work: mode-0600 Unix control socket (reset, personality, dataset, inject, accounts, clock) and armed reply, reorder, and certificate faults

## 2026-11-11 - cyrus probe execution start

- Approved plan: sandbox/plans/cyrus-probe-20261111-0136-plan.md
- Work: separate probe command, operator config example, self-test of one script against two local servers, then match Cyrus 3.13.7 reply shapes for commands this binary already implements
- make stays Docker-free; lab password stays out of the repo and this log

## 2026-11-11 - cyrus probe blocked on a running lab container

- Approved plan: sandbox/plans/cyrus-probe-20261111-0136-plan.md
- liveimap-cyrus-3137 is already running. It was not stopped and not recreated. The Cyrus probe did not run, and the personality was not invented.
- Probe command, empty-password example, self-test, and docs are in the working tree and are not committed.

## 2026-11-11 - probe the running Cyrus container

- Approved plan: sandbox/plans/cyrus-probe-running-20261111-0153-plan.md
- Work: keep the uncommitted probe, run it against the already-running liveimap-cyrus-3137 published IMAP port, and match reply shapes. Do not recreate or stop that container.
- The lab password stays in the gitignored operator file and out of this log.

## 2026-11-11 - Cyrus session probe landed

- Approved plan: sandbox/plans/cyrus-probe-running-20261111-0153-plan.md
- Separate probe matched the already-running liveimap-cyrus-3137 on its published IMAP port. The container was not recreated and was not stopped.
- Personality fields record the Cyrus reply shapes this binary already implements. Unimplemented capabilities stay in personalities/cyrus-3137.gaps.json and are not advertised.
- The lab password stayed in the gitignored operator file.
