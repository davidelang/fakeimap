# AGENTS.md — Project Instructions Bootstrap

This file (AGENTS.md) is the entry point for agent CLIs in this multi-agent VehicleExpenses-automated project.

## Immediate reads (session start / after compact / new cycle)

**Role pack** (full read on each **event** below — law: `AGENT_MANDATES.md` §10):

1. `./AGENT_CONTEXT.md` — identity, branch/role, sandbox.
2. CLI overlay: `./GROK.md` or `./GEMINI.md`.
3. `./AGENT_MANDATES.md` (shared core).
4. `./project-facts.md` (**full** file — orientation only).
5. **If Master:** also `MASTER_AGENT_MANDATE.md`.
6. **If Planner:** `standard-plan-compliance-block.md` (path), `dev-ai-interaction/research/plan-style-guide.md`, designated plan if any.
7. **If Coder (execute):** `standard-plan-compliance-block.md` + **approved** plan path only.
8. **Master / orch after handoff:** also `MULTI_AGENT_USER_INSTRUCTIONS.md`.
9. Confirm **`pwd` once** — never `cd … && ./helper` (breaks allow-lists).

**Events (re-read is mandatory, even mid-process):** launch; **`/compact` or auto-compact** (first turn after); new planning cycle; execute start.

**Skip** only ordinary turns *between* events. “Don’t re-read every turn” **never** cancels a compact. Compacted memory of the pack is untrusted.

Critical rules are not optional “on demand if you guess.”

When **spawning** planner/executor roles, load the full file under `.grok/prompts/` (see AGENT_MANDATES).

**Launchers:** thin `run-grok*` → `.grok/lib/grok-launch-common.sh` + `.grok/prompts/packs/<role>.pack` (compose via `compose-session-prompt.sh`). Same structure on VehicleExpenses and library hosts. Do not re-author law in shell scripts. Sandbox path: `project.config` `sandbox_dir` / `sandbox_path` (VE: `dev-ai-interaction/`, libs: `sandbox/`). Local PR skills: `prepare-local-pr`, `master-merge` (same on all hosts).

## Launchers (role → command)

| Launcher | OS user | Role | Plans? | Implements app? | Native plan mode? |
|----------|---------|------|--------|-----------------|-------------------|
| `./run-grok-orchestrator` | ai-orchestrator | Meta rules / brain | Meta | Meta | Optional |
| `./run-grok-master` | ai-coder | Execute dispatch; PR review/merge | No | Via coder | No |
| `./run-grok-planner` | ai-planner | Long-lived planning; **owns new cycles** | Yes | **No** | **Avoid** |
| `./run-grok-coder` | ai-coder | Implement in agent-N | **No** | Yes (approved plan only) | **No** |
| `./run-grok` | dlang | Bare process-break session | Yes | Yes | Optional |

## Skills

| Enabled (project) | Disabled (do not use for app multi-agent) |
|-------------------|------------------------------------------|
| `prepare-local-pr`, `master-merge`, `rebase-on-master`, `review` (local; shadows bundled GitHub `review`), `check-upgrade` | `pr-babysit`, `execute-plan`, `design`, `check-work`, **`implement`** |

`/code-review` only when user explicitly wants ambitious restructure (separate planned turn).

## Grok 4.6 / Build 1.0 — VE wins (see `AGENT_MANDATES.md` §3.5a)

| Built-in default | VE |
|------------------|-----|
| “Do clear reversible work without asking” | No tracked non-sandbox edits without named-plan magic approval |
| Native plan **`a`** / `exit_plan_mode` starts building | Not execute. Work plan is only `dev-ai-interaction/plans/…-plan.md` |
| Subagents on (incl. during planning) | Planner: `GROK_SUBAGENTS=0`. No spawn while planning. Execute body: fresh child (`execution-subagent.md`, `isolation=none`) or fresh `./run-grok-coder` (no `-c` across plans). Long-lived coder/master **dispatch** that child; they must not implement in the organizer transcript |
| Workflows / `/goal` on | Planner + coder: `GROK_WORKFLOWS=0`. Orch/bare may opt in |
| Shift+Tab → Plan / always-approve | Launchers stay ask. Do not cycle modes on role sessions |
| User/config `xhigh` reasoning | Coder+master launchers `--effort high` (`GROK_REASONING_EFFORT` overrides). Planner/orch unset. Spawned execute child inherits parent |
| `grok -c` uses new default model | `-c` keeps **stored** model; `/model grok-4.6` to switch |

`ask_user_question` stays on; answers are not approval.

## Key file disambiguation

| File | Purpose |
|------|---------|
| AGENT_CONTEXT.md | Role/branch/geography |
| AGENT_MANDATES.md | Shared core law |
| `.grok/prompts/*.md` | Full spawn templates |
| MASTER_AGENT_MANDATE.md | Master merge/execute |
| GROK.md / GEMINI.md | Thin CLI overlays |
| new_agent_prompt | Startup ack + STOP |
| standard-plan-compliance-block.md | Execution gates — **cite path; do not paste** |
| project-facts.md | Orientation map |
| TODO.md | Future backlog (`todo-append` / `todo-close`) |
| ENGINEERING_LOG.md | Activity (`append-to-engineering-log` only) |
| MULTI_AGENT_USER_INSTRUCTIONS.md | Human magic phrases / rituals |
| dev-ai-interaction/ | Sandbox (absolute path preferred) |

## Plans

- Designated file under `dev-ai-interaction/plans/` only. Completed → `historical-plans/`.
- Filenames: `descriptive-kebab-YYYYMMDD-HHMM-plan.md` (**minutes** when stamping).
- Harness session `plan.md` = process log only — never the approved work plan.
- Research findings default to **chat**; files only if asked or durable cache (still discuss in chat).

## Worktree deploy / copy

`./build_app` refuses uncommitted **tracked** dirt. Prefer `./update-rules.sh` (cp + commit). Ad-hoc `cp` of tracked files must be followed by commit on that worktree.

## Coordinates

ICRS or raw pixel integers only. Normalized 0.0–1.0 per-axis is obsolete.

## Next steps after reading

- Fresh launch: Mandate Acknowledgment from `new_agent_prompt`, then STOP & WAIT (except pure role already mid-cycle).
- Implementation only after magic approval naming `dev-ai-interaction/plans/<name>-plan.md`.
- Planning help = research + better plan file, not source changes/builds.
- Sandbox: `/home/dlang/git/VehicleExpenses-automated/dev-ai-interaction/`.

Welcome. Report your role, branch, and mandates understanding now.
