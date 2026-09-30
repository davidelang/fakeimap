# MULTI_AGENT_USER_INSTRUCTIONS.md

**This is the authoritative, tracked, user-facing (human) document for the exact rituals, magic words, and forbidden phrases when interacting with agents (Grok, and by extension the other runtimes) in the VehicleExpenses-automated multi-agent orchestration.**

It lives at the orchestration root and is physically synced (via `update-rules.sh`) into `master/` and every `agent-N/` worktree so it is always present and versioned alongside AGENTS.md / AGENT_MANDATES.md.

Read this (and the referenced sections in AGENT_MANDATES.md and AGENTS.md) before giving any directive after a handoff or when starting new work with an agent. The goal is reliable plan/execute cycle boundaries: the agent produces a visible plan file in the sandbox that you review and explicitly approve by path; after handoff the turn is over and the next input starts a brand-new planning cycle.

## 0. Two-Terminal Long-Lived Workflow (Primary Recommended Mode) — Minimal Human Ritual

Keep two terminals open:
- Master: `./run-grok-master` (coordination only)
- Planner: `./run-grok-planner` (all research + plan document work)

**To begin or restart planning after a handoff:**
1. In the **master** terminal say the short trigger:
   ```
   New planning cycle
   ```
   or
   ```
   New planning cycle. [one short sentence: goal or feedback from last test / project-facts.md]
   ```

2. The master (automation is built into its initial prompt) will:
   - Read project-facts.md + latest `standard-plan-compliance-block.md`
   - Write a complete narrow prompt file to `@@SANDBOX_PATH@@/.planning-agent-prompt.txt` (full planner template + embedded current guardrails block + your context)
   - Tell you: "New planning cycle. Please restart your planner terminal now with the updated prompt file (run `./run-grok-planner` or `exec ./run-grok-planner`)."
   - Stop (it will not plan or research)

3. In your **planner** terminal run (or `exec`):
   ```
   ./run-grok-planner
   ```
   Talk directly to the planner for everything until you have a plan file ready to approve.

4. When ready for execution, return to the **master** terminal and use a magic approval phrase naming the exact plan file (see section 2).

The detailed instructions for constructing the planner prompt and the exact output the master must give have been moved into the master's built-in prompt (see `run-grok-master`). You should rarely need to look up long meta-phrases.

## 1. After Any Handoff ("results ready to test", END OF EXECUTION TURN marker, or `./build_app` success that the agent announces)
The prior execution turn is **finished** (per AGENT_MANDATES "Completion and Handoff (CRITICAL)" and "Plan File Access..." rules). Any feedback you give is the start of a *new* planning/research cycle. The agent must treat it as such.

**Recommended (cleanest, gives forced clean slate every time):**
- Exit the current agent CLI session completely.
- Relaunch with the normal command, e.g.:
  ```
  cd <agent-dir or feature-name.wt>
  ../run-grok
  ```
  (or the equivalent for run-gemini / run-antigravity).
- Every launch via `run-grok` (etc.) injects the full fresh-session instruction. The agent will produce the Mandate Acknowledgment report, enter plan mode via the tool, and STOP. You then give your new request as the "specific directive".

This eliminates context bloat, accumulation in the harness `~/.grok/sessions/.../plan.md`, and "asking to approve things already done".

**If you prefer to stay in the same long chat session (fallback):**
- The agent is *required* (as part of its handoff completion steps) to write the current short gate text to `@@SANDBOX_PATH@@/.post-handoff-gate.txt`.
- Use this trivial ritual (one-liner in your shell or copy-paste):
  ```
  cat @@SANDBOX_PATH@@/.post-handoff-gate.txt
  ```
  Then immediately append (or type after) your actual new feedback or request.
- The gate file text is intentionally short. All the detailed rules, magic words, and "never say" lists are in this document + the re-read AGENT_MANDATES.md.

The agent must also have created (or the user explicitly designated) a fresh sandbox plan file for the *prior* cycle; your new input will cause it to create a *new* one under `@@SANDBOX_PATH@@/plans/` for this cycle.

## 2. Magic Words / Required Phrases the User *Must* Use for Approval (these count as the "explicit Directive")
When you are ready to let the agent proceed from planning to execution, your approval message **must** reference the exact sandbox plan file path and use one of the following (or very close unambiguous equivalents). The agent is instructed to treat only these as authorizing source changes for that plan.

- `approved the plan at @@SANDBOX_PATH@@/plans/<exact-filename>.md for the following request: [paste or clearly describe the work]`
- `The user approved the plan at @@SANDBOX_PATH@@/plans/xxx-plan.md. Proceed with execution of exactly the steps described in that plan only.`
- `approved, use the plan at @@SANDBOX_PATH@@/plans/my-task-20260612-plan.md for: [summary]. First action update ENGINEERING_LOG.md then follow the phased steps with forensic reads and build at milestones.`

After you send one of the above, the agent may exit plan mode (if still in it), update ENGINEERING_LOG.md via `./append-to-engineering-log` (first execution action; the master ensures wrappers are used for initial ENGINEERING_LOG entries), re-read the designated plan file, and begin the small decomposed steps.

The approved plan **Phased Execution** section must use coherent, independently verifiable phases (~3–8 typical for modest features). Each phase states what changes, which files, and observable success — not a repeated gate checklist (the STANDARD BLOCK covers forensic verification + `git add` + successful `./build_app` before the next phase). On trouble, follow the **Baseball Rule** in AGENT_MANDATES.md (3 strikes = out; 3 outs = end of inning → End of Inning Report in `implementation-failure-logs/` before replan). On partial reset, only the tag of the most recent successful phase (after `./get-builds-tag.sh` preflight) may be used.

**Never rely on "looks good", "go ahead", "sounds right", "implement the changes", or similar vague language alone.** The path + "approved the plan at ..." is what the rules require for an unambiguous handoff from planning to execution.

## 3. Phrases the User Must *Never* Say (or Types of Feedback to Avoid) After a Handoff or When Starting a New Request
These have repeatedly been misinterpreted by agents as permission to continue editing the just-handoff'd plan, treat the message as continuation of the prior execution turn, skip creating a fresh sandbox plan file, or bypass the enter_plan_mode + exit + explicit path approval gate.

**Never say these (or close variants) in a message that follows an END OF EXECUTION TURN / "results ready to test" / handoff announcement without first giving a fresh approved plan path using the magic phrasing above:**

- "do you have any questions about this task"
- "can you also..." / "also..." / "and while you're at it..."
- "just fix the..." / "just make the..." / "can you make the..."
- Any specific result description of the prior turn's output used as if it is still the same turn, e.g. "the red boxes are not larger", "the html for set C is showing broken links", "the binarization of set C does not appear to be happening", "nested boxes are still present", "the deskew rotation of set C seems to be rotating the wrong direction"
- "looks good but..." / "almost there but..."
- "continue with..." / "keep going on..."
- "what about X" or "try Y" (as a direct follow-up without "approved the plan at <new path>")
- "the feedback came at the end of the turn / while I was testing so it is still part of the same execution turn"
- Any phrasing that could be read as "the previous plan was only high-level so you can fill in details now" or "small additional edit is fine"

If you want to give test observations or corrections on a just-handoff'd turn, first say the magic approval for a *new or revised* plan that incorporates the feedback (or say "relaunch and read the new request below" and give the feedback as a fresh directive). The agent is instructed that post-handoff feedback = start of new planning cycle; it must produce (or be given) a new designated sandbox plan before any further source edits.

See AGENT_MANDATES.md "No Loophole Hunting or Rationalization (CRITICAL)" and "Execution handoff rule" for the exact policy the agent must follow (and self-report violations of).

## 4. Quick Checklist for Every New Request or Post-Handoff Message
1. If this follows a handoff/END marker: prefer relaunch (`../run-grok`). Fallback: `cat @@SANDBOX_PATH@@/.post-handoff-gate.txt` then your text.
2. In planning the agent must create a *new* plan file under @@SANDBOX_PATH@@/plans/ (not supersede or continue an old one in the harness session plan.md). Read (and help maintain) `project-facts.md` in the worktree root. It holds stable layout and location facts only.
3. When ready to execute: use one of the magic approval phrases in section 2 that names the *exact* sandbox plan path.
4. After the agent says results are ready + END marker + has run `./build_app`: the turn for that plan is over. Treat your next input as a new cycle (relaunch or short gate ritual).
5. Never read or let the agent rely on historical plans/ or the harness session plan.md as the approved contract for work.
6. If the agent appears to be continuing edits without a new approved path, remind it of the END marker / this document and the handoff rules; it must self-report, revert if needed, enter plan mode, and wait for a fresh directive + sandbox plan.

## 4.5 Interactive Strategic Planning and Low-Cost Continuity (using local per-worktree state)
The pre-approval planning phase is the interactive strategic layer. "Being helpful," "being proactive," "being efficient," or similar **does not** mean the agent should make source changes or run builds/compiles. It means the agent should do research, suggest ideas, and make the plan document better.

**How to give feedback without triggering "abandon plan":**
- For continued discussion on the current draft: "This is more feedback on the plan at @@SANDBOX_PATH@@/plans/xxx-plan.md. Revise the plan document and produce an updated version at @@SANDBOX_PATH@@/plans/xxx-v2-plan.md (or the same file) addressing: [your points]. Do not call exit_plan_mode yet."
- Only use language like "start over", "new plan", "completely abandon this plan", or "new cycle from scratch" if you actually want a full reset.

You are encouraged to give rich problem descriptions, high-level direction, and detailed iterative feedback on draft plan documents (e.g. "The plan at @@SANDBOX_PATH@@/plans/xxx-plan.md correctly identifies the issue but under-specifies Y. Revise and write the updated plan to @@SANDBOX_PATH@@/plans/xxx-v2-plan.md addressing: [your details].").

For significant or long-running work, start the top-level coordinator using the dedicated master launcher:

```bash
./run-grok-master          # from the orchestration root
../run-grok-master         # from inside a worktree
```

This launches the Master Orchestrator with a role prompt that makes it fully aware of the entire scheme. Its responsibilities include:
- Coordinating planning by maintaining a long-lived dedicated Planning Agent terminal (via `run-grok-planner`). When the master wants to start a new planning cycle it writes a fresh narrow prompt (including the current standard block and high-signal rules) to `@@SANDBOX_PATH@@/.planning-agent-prompt.txt` and tells you to restart your planner terminal with the updated file. This gives you direct, unmediated conversation with the Planning Agent (no relay) for research and plan revision across many cycles while the master handles overall coordination, monitoring, and execution handoff.
- Only considering a plan approved when you use the exact magic approval phrasing that names the file in `@@SANDBOX_PATH@@/plans/`.
- Spawning a narrow Execution Sub-agent (via `spawn_subagent`) with the approved plan injected.
- Actively monitoring the implementation sub-agent for run-away behavior (excessive/improper resets without using `get-builds-tag.sh`, continued editing after the END OF EXECUTION TURN marker, violating the approved plan, etc.).
- If deviation is detected: stop the sub-agent, force a clean reset to the last good builds tag, collect detailed logs of everything the implementation agent attempted (into `@@SANDBOX_PATH@@/implementation-failure-logs/`), launch a new planning round (by updating the prompt and telling you to restart the planner terminal), and feed the logs + a clear "analyze what went wrong and produce a recovery plan" request to the planning agent.

**Primary recommended workflow (long-lived terminals) — now heavily automated in the master prompt:**

When you start a new planning cycle with a question about the code ("how does the current dispatch work?", "where is the valley logic?"), the dedicated planner is instructed to treat this as pure research: it should investigate with tools and answer you directly in the conversation. It should **not** spend time creating a formal plan file in `@@SANDBOX_PATH@@/plans/` or spawning sub-agents unless you later say the work involves making changes and you want a plan for implementation. 

A formal plan is required for *any* work that will touch tracked files outside the sandbox (i.e. real source code in the main app directories that will be committed and built). The planner is free to work inside the sandbox (`@@SANDBOX_PATH@@/`) and on `project-facts.md` (and TODO.md) without needing its own plan document for every small step.

Formal sandbox plans are for cycles that involve code changes / refactoring that will need approval and execution. Research questions (or pure sandbox work) do not require one unless you explicitly ask for a plan.

Keep one master terminal (`./run-grok-master`) and one dedicated planning terminal (`./run-grok-planner`) open across cycles. The planner is started once.

**To start (or restart after handoff) a planning cycle — the only thing you usually say to the master:**
- In the *master* terminal say exactly (or very close):
  `New planning cycle` 
  or
  `New planning cycle. [one short sentence of goal or feedback from the last test / project-facts.md]`

- The master (whose prompt now contains the full automation) will:
  - Read project-facts.md + the latest `standard-plan-compliance-block.md`.
  - Write a complete, ready-to-use narrow prompt (including the full dedicated planner template + embedded current guardrails block + your context) to `@@SANDBOX_PATH@@/.planning-agent-prompt.txt`.
  - Tell you the single line: "New planning cycle. Please restart your planner terminal now with the updated prompt file (run `./run-grok-planner` again in that terminal, or use `exec ./run-grok-planner` to replace the process in place)."
  - Then stop. It will not do research or planning itself.

- Immediately switch to (or restart in) your planner terminal with `./run-grok-planner` (or `exec ./run-grok-planner`).

**Give the actual detailed problem description directly to the planner terminal.** The short trigger to the master is deliberately minimal (just enough to load the current rules, hygiene, and a rough topic into the planner's prompt). The planner has been explicitly instructed *not* to treat a one-line trigger as sufficient requirements. It should ask you for the real problem statement, goals, constraints, etc., before doing research or writing a plan. All the interactive back-and-forth and plan iteration happens directly with the planner.

- When ready for execution, return to the master terminal and use one of the exact magic approval phrases that names the plan (see section 2 below), e.g. "The plan at @@SANDBOX_PATH@@/plans/xxx-plan.md is now approved. Execute it."

This is the expected primary mode. The detailed "what to say" logic has been moved into the master's initial prompt so you have less ritual to remember.

This is the expected primary mode of operation for most users. The same hygiene rules (aggressive pruning of `project-facts.md`, facts-and-pointers only, light reporting during mechanical steps) remain mandatory on every cycle even though the terminals are long-lived.

For lighter or one-off work you can still use the ordinary `run-grok` (or ask the master for an in-session Planning Sub-agent via tool). All feedback in that case still routes through the main agent.

**Restart ritual for new cycles (now largely automatic):** After any handoff (or to begin fresh work), just say the short trigger in the master terminal ("New planning cycle" + optional one-liner). The master prompt is built to handle writing the full `.planning-agent-prompt.txt` (with current block and hygiene rules) and giving you the exact one-line restart instruction. You then restart the planner terminal and talk to it directly.

**Important:** Give the substantive problem details (what exactly is wrong, what success looks like, constraints, etc.) to the *planner*, not the master. The planner is now strongly prompted to wait for your detailed input rather than autonomously generating a large plan from a tiny trigger.

This gives you direct, unmediated conversation with the "planning sub-agent" until you explicitly approve and exit. The main agent only gets involved for orchestration, final approval handoff, and execution.

To keep re-familiarization cheap across cycles in long-lived terminals:
- Each worktree root has a tracked `project-facts.md`. It holds *only stable layout facts and key "where things live" locations* that would still be true after the current effort is merged and a new worktree is created for different work. No plan references, branch, tags, or "working on" (see AGENT_MANDATES "project-facts.md Content and Hygiene Rules").
- On every new planning cycle, include language such as: "Continue strategic planning for the work described using project-facts.md (this worktree) and the previous plan at @@SANDBOX_PATH@@/plans/xxx-plan.md. New feedback: [details]. Produce a revised plan at @@SANDBOX_PATH@@/plans/yyy-plan.md." (You may also say "perform hygiene on project-facts.md first" if needed.)
- The Planning Agent and the master must read local `project-facts.md` first at the start of each new cycle (for stable locations and layout) + the designated plan file. **project-facts.md must hold only stable facts/locations** that survive merges and new efforts — no plan text, execution steps, branch/tag, or "I'm working on". See AGENT_MANDATES. Detailed effort goes to ENGINEERING_LOG.md (append-only; use `./append-to-engineering-log` wrapper exclusively).
- **Hygiene before editing project-facts.md**: (1) read the *entire* file, (2) if large report size for separate cleanup, (3) remove non-stable items. Keep focused on enduring locations so planners avoid discovery commands.
- For long mechanical execution plans, use *light reporting* in visible responses and put step details + "I'm on step X" into ENGINEERING_LOG.md (dated; via `./append-to-engineering-log` wrapper). project-facts.md only gets new codebase facts if discovered.

This gives you low-cost interactive guidance without paying full re-derivation cost on every turn, while the safety rules (written sandbox plan + explicit magic approval) remain in force. The local state file travels with the branch/worktree and is never treated as a substitute for the approved plan document. Long verbose state or response bloat is now a violation of the content/hygiene rules.

## 5. References (Always Re-Read These on Fresh Launch / After Compaction / Post-Handoff)
- `AGENT_MANDATES.md` (the authoritative shared core, especially Bi-Modal Workflow, Completion and Handoff, Plan File Access and Discovery Rules, Sandbox Plan File as Primary Artifact, Harness Session plan.md Lifecycle and Hygiene, Planning and Execution Subagent Separation, Post-Handoff Cycle Start Protocol).
- `AGENTS.md` (bootstrap, Plans Directory Rule, geography).
- `new_grok_agent_prompt` (the report the agent must give on fresh launch or after compaction; it now includes explicit confirmations about the sandbox plan file, harness log hygiene, and post-handoff behavior).
- `MULTI_AGENT_USER_INSTRUCTIONS.md` (this file — the human rituals).
- The specific sandbox plan file the user has most recently and explicitly designated for the *current turn* only.
- Existing example plans in @@SANDBOX_PATH@@/plans/ (for structure): `buffer_set_lock_removal_plan.md`, `jpg_removal_plan.md`, etc.

## 6. Notes for Agent Implementers / This Document's Maintenance
This file is maintained in the orchestration root (like the other core instruction files) and distributed by `update-rules.sh`. When the protocol or rituals evolve, update this file + the corresponding sections in AGENT_MANDATES.md / AGENTS.md / new_grok_agent_prompt, then run the sync from the orchestration root on the orchestration branch.

Agents must treat the lists here as non-negotiable (the "letter and spirit" of the handoff boundary rules).

(End of MULTI_AGENT_USER_INSTRUCTIONS.md. Created as part of the approved meta-plan for robust plan/execute cycle enforcement.)
