# Grok Project Mandates (Overlay)

Thin overlay. Shared law: `AGENT_MANDATES.md`.

**Grok CLI:**

- Tools: Read, Write, StrReplace, Shell, Task/spawn, enter/exit plan mode.
- **Shell cwd:** `pwd` once at startup. Invoke `./append-to-engineering-log`, `./build_app`, `./get-builds-tag.sh` as literal `./helper` — **never** `cd … &&`.
- **Native plan mode:** Optional only for bare `./run-grok` and orchestrator. **Planner and coder must not rely on it** (role barrier + mandates). Approved work plan is always a sandbox file under `dev-ai-interaction/plans/`; harness `plan.md` is process log only.
- **Planning:** Research + revise sandbox plan only. Do not call `exit_plan_mode` until user path-approves (or says so). “Helpful” ≠ implement or build.
- **Execution:** Only after magic approval of exact plan path. Completeness pass before handoff. Plan Status APPROVED → CODE LANDED.
- **Spawn prompts:** Load full `.grok/prompts/planning-subagent.md` / `execution-subagent.md` / `dedicated-planner.md` when spawning — do not invent weaker prompts.
- Coordinates: ICRS or pixel only.
- Git reset: three contexts + `./get-builds-tag.sh` preflight.
- No deployment.
- Worktree deploy: no uncommitted tracked dirt after `cp` (use `update-rules` or commit).
- **Permission denials:** `AGENT_MANDATES.md` §1.1 — report to human; no creative workarounds (chmod/chown games, identity laundering, path dodges, Landlock disable).
- **4.6 / Build 1.0 overrides:** `AGENT_MANDATES.md` §3.5a + table in `AGENTS.md`. “Just do it,” native plan **`a`**, and question-card answers are not VE execute. `ask_user_question` stays on. `-c` keeps stored model. Coder/master launchers pin `--effort high`; the organizer parent does not implement (spawn `execution-subagent.md`).

**Re-read after compaction** (`/compact` **or** auto-compact): role pack in `AGENTS.md` on the **first turn after** the event — even mid-plan/execute. “Not every turn” does **not** apply to compact (`AGENT_MANDATES.md` §10).
