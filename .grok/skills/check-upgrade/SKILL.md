---
name: check-upgrade
description: >
  After a Grok Build / CLI upgrade (or a default-model change), scan built-in
  skills, slash commands, personas, permission-grant behavior, and product
  biases against this repo's multi-agent / multi-worktree policies. Use when
  the user upgrades grok, says "check the upgrade", "new grok version",
  "built-in skills to disable", "1.0.x", or /check-upgrade. Chat report plus
  optional sandbox log; do not edit tracked law until a named plan is approved.
when-to-use: "upgrade grok, check-upgrade, new Grok Build, disable built-in skills, personas, permission defaults, model bias, /check-upgrade"
user-invocable: true
---

# check-upgrade

Scan a **new Grok Build / CLI** (and any **new default model**) against **this
host's** multi-agent law. Same skill on VehicleExpenses and library hosts.

This file is the **procedure + what to walk**. It does **not** restate law.
Cite, do not paste:

- `AGENTS.md` (roles, enabled/disabled skills, VE-wins table)
- `AGENT_MANDATES.md` §1.1, §2–§3, §3.5a, §10
- `GROK.md` / `ANTIGRAVITY.md`
- `.grok/lib/grok-launch-common.sh`, `run-grok*`
- `.grok/config.toml` (`[skills].disabled`, `[permission]`, hooks)
- `MULTI_AGENT_USER_INSTRUCTIONS.md`
- `$SANDBOX/research/check-upgrade-log.md` (prior upgrade decisions)

If the log or a cited section is missing, say so. Do **not** invent the last
upgrade from memory.

## Preconditions

- Human already installed the binary (or states from→to). You do not run
  `grok update` / `grok-install.sh`.
- **Planning only:** sandbox writes. Tracked law/launcher/config edits need
  a **new** named plan + magic path approval.
- `pwd` once. No `cd … && ./helper`.

## Resolve paths

1. `project.config` → `sandbox_dir` / `sandbox_path` → **`$SANDBOX`**.
2. Else `./dev-ai-interaction` or `./sandbox`.
3. Product docs: `$GROK_HOME/docs/user-guide/` or `~/.grok/docs/user-guide/`.
4. Launcher binary: `grok_bin` / `grok_bin_default` (this host often
   `$HOME/git/grok/bin/grok`, not `~/.grok/bin/grok`).
5. Bundled skills: `$GROK_HOME/bundled/skills/` or the binary's bundled tree.
6. Bundled personas/agents: `~/.grok/personas/`, `.grok/personas/`,
   `.grok/agents/`, plus whatever the new user-guide names.

## Steps

1. Record **from → to** (`grok --version` on the launcher binary), channel,
   and **current default model** (`/model` list or config).
2. Read new user-guide files that exist: getting-started, slash-commands,
   configuration, plan-mode, sandbox, skills, plugins, hooks, subagents /
   personas, workflows, permissions-and-safety, sessions/dashboard, memory.
   Skim `grok --help` and new top-level commands.
3. Inventory **new or changed** bundled skills (names + descriptions).
4. Inventory **new slash commands** and **new CLI flags**.
5. Inventory **new/changed personas** and agent types.
6. Walk **every category below**. Mark each item:
   `unchanged` | `weaken-VE` | `disable-candidate` | `useful-opt-in` |
   `bias-to-counter` | `persona-fit` | `irrelevant`.
7. Report **in chat** using the output shape. Also append one dated section
   to `$SANDBOX/research/check-upgrade-log.md`.
8. If law/launchers/`[skills].disabled`/permission rules must change: write
   a **new** `$SANDBOX/plans/…-YYYYMMDD-HHMM-plan.md`. Do not implement.
9. If this upgrade (or the human) names a **new category**, record it in
   `$SANDBOX/research/check-upgrade-log.md` and write a follow-on named
   plan if `.grok/skills/check-upgrade/SKILL.md` must change. A scan must
   **not** edit the project skill without magic path approval.

## Categories (always walk)

### 1. Built-in skills that conflict with policy

Do **not** disable a skill merely because it is powerful. Disable only if:

1. **Agents will pick it instead of a VE process** — broad `when-to-use` /
   description triggers (“review my changes”, “open a PR”, “implement this”)
   and it is *not* explicit-only (`disable-model-invocation` / slash-only),
   **or**
2. **The skill’s behavior is wrong here even when the human types `/name`**
   — GitHub/Graphite PR create/babysit vs `prepare-local-pr`; product
   `/implement` vs named sandbox plan + coder; harness `plan.md` as the
   work plan; deploy; remote merge.

Explicit-only skills that stay on the VE rails (example: `/code-review`
when the human asks for an ambitious restructure) stay **enabled**.

For **every** bundled skill, record: auto vs slash-only; whether it
substitutes a VE ritual; disable? yes/no + one-line why.

Current VE **disabled** (keep unless the human opts in): `pr-babysit`,
`execute-plan`, `design`, `check-work`, `implement`.

Enabled project skills today: `prepare-local-pr`, `master-merge`,
`rebase-on-master`, `review` (local; shadows bundled GitHub `review`),
`check-upgrade`, `validate-plans`.

### 2. New commands useful in multi-agent / multi-worktree

Flag commands/flags that help **this** layout (orch + `master/` + `agent-N`
+ lib hosts + Landlock), e.g. worktree, dashboard, session fork/resume,
minimal/no-alt-screen, model pin, permission-mode visibility.

Flag commands that **look** useful but collide with VE (native `/plan`,
`/always-approve`, `/auto`, `/goal`, `/workflows` on planner/coder, product
`--worktree` vs real `master/` merge).

### 3. Model change — biases to review and counter

When the **default or recommended model** changes (or `/model` roster
gains a new coding default):

- "Do clear reversible work without asking"
- Treating question-card answers as execute approval
- Planning-time subagents / background agents
- Preferring harness plan mode over `$SANDBOX/plans/`
- Web-stack defaults (this repo is native Android/Kotlin/Gradle)
- Silent path/username fallbacks (mandates §9.1)
- Compaction amnesia ("don't re-read every turn" used to skip §10 events)

Cite `AGENTS.md` VE-wins + §3.5a. Propose **counter-text** (overlay /
launcher env / mandate bullet) only in a follow-on plan.

### 4. Personas and built-in agent types

- New bundled personas (`/personas`, `[subagents.personas]`, `.grok/personas/`)
- New agent types (explore/plan/general-purpose/…) and whether they
  match planner / coder / master / orch
- Can a persona or type **escape** plan-mode edit gates or inherit
  always-approve? (Product already documents subagents ignoring parent
  plan-mode.)
- Do **not** turn on personas in role launchers unless the human asks.
  If one matches a role, say so as **opt-in**.

### 5. Permission-grant and tool-policy changes

- Default permission mode (ask / auto / always-approve / yolo)
- Shift+Tab cycle (Plan / Always-approve) vs launchers staying **ask**
- New allow/deny rule syntax; project vs user vs managed config merge
- Hooks that auto-approve or widen grants
- `capability_mode` defaults
- OS sandbox (`GROK_SANDBOX`) vs session **Landlock** (`agent-landlock`)
- §1.1 still wins: no chmod/chown/identity-launder on denials
- Headless / ACP `--always-approve` leaking into interactive role sessions
- **Per-OS-user `[ui] permission_mode`** (1.0.11+; user-scope; project
  `.grok/config.toml` cannot override). Read **each role user’s**
  `~/.grok/config.toml` (`dlang`, `ai-orchestrator`, `ai-planner`,
  `ai-coder`). VE `run-grok*` pin `--permission-mode default`
  (`GROK_PERMISSION_MODE` overrides). If the pin is missing and user
  config is `always-approve` / `auto`, that is a VE-wins miss. Bare
  `grok` (no launcher) still follows the user file. Do not use a
  project `[ui]` key. 1.0.20+: spawned subagents **inherit** parent
  always-approve — pin on the parent session is what children get.

### 6. Execute / plan barrier (product vs VE)

- Native `/plan`, TUI **`a`**, `exit_plan_mode` starts building
- Harness `plan.md` vs `$SANDBOX/plans/…-plan.md`
- `ask_user_question` ≠ magic approval
- Workflows, `/goal`, `/deep-research` (planner+coder stay
  `GROK_WORKFLOWS=0` unless overridden)
- Subagents during planning (`GROK_SUBAGENTS=0` on planner)

### 7. Worktrees, sessions, memory

- Product `--worktree` / `GROK_WORKTREE` vs VE `master/` + `agent-N`
  (master: product worktrees = merge **dry-run** only)
- Dashboard / fork / attach / multi-session vs two-terminal ritual
- Memory (`/memory`, `/flush`, `/dream`) vs `project-facts.md` +
  eng-log + TODO wrappers
- Compact / auto-compact still requires a **pack re-read** (§10)

### 8. Hooks, MCP, plugins, marketplace

- New default hooks; Stop/completeness hooks (`VE_STOP_COMPLETENESS`)
- New MCP tools that write remotes or tickets
- Marketplace auto-install / auto-trust
- Plugin skills colliding with `/prepare-local-pr` or `/master-merge`

### 9. Install, binary, config geography

- `grok update` vs customized `grok-install.sh` (`GROK_BIN_DIR`,
  `$HOME/git/grok`)
- Completions, `managed_config.toml`, `[cli]` version pins / auto_update
- User `~/.grok/config.toml` vs project `.grok/config.toml` precedence
- Do not replace the running binary from a landlocked session

### 10. Multi-host blast radius

- VE orch, `master/`, agent worktrees
- Lib hosts (`remotetable`, `extractmail`, orchestration-example):
  launchers/skills/landlock only — not VE Gradle wrappers
- Recommend disable/enable **per role** if a new default is orch-only

### 11. Suggested extras (walk if present in the new build)

- Background `/loop`, monitors, schedulers (foreground vs landlock)
- Image/video/voice tools (usually irrelevant; note if they grant
  unexpected network/fs)
- Telemetry / dashboard analytics defaults
- New coordinate or browser tools (VE: ICRS/pixels only)
- Completeness or "are you done?" product gates vs VE END marker
- Truncated replies auto-continuing; truncated tool calls executing
  when arguments look complete
- `grok update --stable` / `--alpha` vs this host’s
  `$HOME/git/grok/config.toml` `channel`
- **Running process vs on-disk binary:** after `grok update`,
  `/proc/<grok-pid>/exe` may show `(deleted)` while
  `$HOME/git/grok/bin/grok --version` is newer. Role sessions pick up
  the new binary only on a **new process** (not `-c`). Record both.

### 12. Idle recaps, turn summaries, compact summaries

Named after agents treated idle recaps as session facts.

- `features.session_recap` / `GROK_SESSION_RECAP` — after idle, an LLM
  one-liner (`Recap —`) from a trimmed snapshot (includes compact
  summaries). Listing stores `last_recap` in `summary.json`.
- `features.turn_summary` / `GROK_TURN_SUMMARY` — `last_turn_summary`.
- `/recap` **generates** a recap; it is not an off switch.
- Compact continuation (`This session is being continued…`,
  `synthetic_reason: compaction_meta`) is injected as a **user**
  message. Untrusted. Pack re-read after compact still required (§10).
- Frozen `generated_title` / `session_summary` can stay days-old.
- **Do not** cite recaps or compact summaries as law, git state, or
  plan status.
- Disable recap/turn-summary: that OS user’s `~/.grok/config.toml`
  `[features] session_recap = false` and `turn_summary = false`
  (config watcher may apply without process restart). Env
  `GROK_SESSION_RECAP=0` / `GROK_TURN_SUMMARY=0` is process-start.
  Project `.grok/config.toml` does **not** load `[features]`.

## Output shape (chat)

```
Upgrade: <from> → <to>  binary=<path>  default model=<id>
Disable-candidates (built-in skills/commands): …
Useful-opt-in (multi-agent / multi-worktree): …
Bias / model counters to review: …
Persona / agent-type fits (opt-in only): …
Permission-grant changes: …
Weaken-VE if we adopted the default: …
Unchanged VE wins: …
Proposed plan (if any): $SANDBOX/plans/…
Log: $SANDBOX/research/check-upgrade-log.md
```

Keep the chat high-signal. Put long inventories in the log file.

## Forbidden

- Tracked edits to mandates, launchers, or `.grok/config.toml` without
  a named approved plan
- Treating "looks good" or a question-card as execute approval
- Agent-run `grok update` / install / `update-rules` sweep
- Enabling a new bundled skill or persona "to be helpful"
