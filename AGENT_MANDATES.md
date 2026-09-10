# Agent Mandates (Shared Core for All CLIs)

This is the authoritative shared core for Grok, Gemini CLI, Antigravity, and future agent runtimes in the VehicleExpenses-automated multi-agent orchestration.

Agent-specific thin overlays (`GROK.md`, `GEMINI.md`) add CLI tool mappings and startup notes. They reference this file for common rules.

**Spawn prompt files** (load full text when spawning; do not paste into plans):
- `.grok/prompts/planning-subagent.md`
- `.grok/prompts/execution-subagent.md`
- `.grok/prompts/dedicated-planner.md`

Human rituals: `MULTI_AGENT_USER_INSTRUCTIONS.md`. Execution gates: `standard-plan-compliance-block.md` (cite by path; do not paste).

---

## 1. Precedence and foundational bans

Overlays + this file take absolute precedence. Bypassing protocol for speed is a **High-Severity Performance Failure**.

- An **approved plan never** authorizes foundational violations (deploying; `git commit --amend`; moving or rewriting `works` / lifecycle tags outside rules).
- Lifecycle tags (`builds`, `deployed`, `works`) are **branch-prefixed** except on `master` (`builds`).
- **No agent deployment:** no `./deploy`, `./gradlew installDebug`, or `adb install`. User deploys; agents fetch logs next turn.
- Version integrity: commit via `./build_app` before builds that matter for `git describe`. Prefer `./build_app @phase_summary.txt …` for multi-line phase summaries; single-line `-m` only for trivial steps. Plan + eng-log + git carry “why”; tags carry state.
- Native Android/Kotlin/Gradle — ignore default web-stack advice.

### 1.1 Permission denials — report; do not work around

If a Unix DAC, Landlock, sudo, group-membership, or filesystem permission denial blocks a required action, **stop and report to the human**. Do **not** invent creative workarounds.

**Report (in chat):** exact command, full error text, path(s), OS user (`whoami` / `id`), and whether a known recovery script already exists (name only — do not run destructive host fixers unless the user asked).

**Forbidden workarounds (non-exhaustive):**

- `chmod` / `chown` / `setfacl` games to “make it work”
- Alternate paths, copies, or bind-mounts to dodge the denied path
- Variable/indirection tricks so a denied or non-allow-listed form still “matches”
- Switching OS user / `sudo -u` / `sg` / nested shells to launder identity
- Running as a different role’s tree to bypass multi-user git or sandbox DAC
- Disabling Landlock, widening grants, or editing `landlock.config*` ad hoc unless the user ordered that change
- Treating “I can still make progress somehow” as success when the mandated path failed

**Allowed:** re-try the **same** mandated helper/path after the human fixes host state; use project wrappers already intended for the action (e.g. `./append-to-engineering-log`); read-only diagnosis the agent can run without escalating privileges.

**Rationale:** Multi-user DAC (`ai-shared` vs `ai-code`, setgid `.git`, Landlock) is load-bearing. Silent bypasses re-break planner/coder isolation and hide real defects (see multi-user git repair docs / `./fix-multiuser-git-hosts.sh --audit-only`).

This is **policy** (this file). Orientation map only: `project-facts.md`.

---

## 2. Write authority

### 2.1 Planning (until magic approval of a named sandbox plan)

**May write:** sandbox  
`/home/dlang/git/VehicleExpenses-automated/dev-ai-interaction/`  
(and `./dev-ai-interaction`), plus:

- `project-facts.md` (orientation only — §7)
- `TODO.md` **only** via `./todo-append` / `./todo-close`

**Must not:** edit other tracked files outside the sandbox; run product `./build_app` / implement app changes.

**Helpfulness in planning** = research, alternatives, better plan document — **not** source changes, builds, or “just one fix.”

### 2.2 Execution (after user magic-approves exact `dev-ai-interaction/plans/…-plan.md`)

Only that plan’s observable contract. First action: `./append-to-engineering-log`. Re-read `standard-plan-compliance-block.md` at execute start (cite in plans; never paste).

### 2.3 Pure research

Explain/where/how questions with no change intent: answer in **chat** with tools. No formal plan until user steers toward non-sandbox tracked edits.

### 2.4 Research communication

Default: findings in **chat**. Files only if user asks or durable cache/handoff is required — still **state issues in chat**; never assume the user read a file.

---

## 3. Bi-modal boundary

| Mode | May | Must not |
|------|-----|----------|
| **Planning** | Research; sandbox plans; allowed special-file edits | App source; product builds; vague “go ahead” as approval |
| **Execution** | Edits per approved plan only | Scope creep; treat post-handoff feedback as same turn |

**Approval:** Explicit user phrase naming the **exact** sandbox plan path (`MULTI_AGENT_USER_INSTRUCTIONS.md`). Vague approval is insufficient.

### 3.1 Mid-execution product feedback (keep force)

If, **during** execution (including after some edits), the user says the result does not match intent — e.g. wrong layout, missing behavior, “still broken,” “not larger,” etc. — that is **not** permission to keep debugging/editing as the same approved turn. It means the plan was insufficiently precise. Finish only true plan-completeness gaps already in the contract, or hand off / stop for a **new** plan. Do not invent scope from conversational corrections mid-flight.

### 3.2 Handoff ends the turn

After plan-scope complete (§4), forensics, successful `./build_app`, and explicit ready-to-test / **exact** END marker from `standard-plan-compliance-block.md`, the turn is **over**. Further feedback = **new planning cycle**. No exceptions for “small tweak,” “still same turn,” “user is testing,” or “rules don’t forbid….”

### 3.3 Letter and spirit (non-exhaustive forbidden rationalizations)

Follow boundaries in letter and spirit. You may **not** justify illegal edits/builds with arguments such as:

- “Not explicitly forbidden…”
- “Plan was high-level; feedback lets me fill details”
- “Feedback after I said complete / while testing is still this turn”
- “One more small edit”
- “Session/historical plan.md authorizes work”
- “Helpful/proactive/efficient means implement/build now”
- “Variable-wrap or indirect a whitelisted command so the allow pattern still ‘works’”
- “Don’t re-read every turn, so skip the pack after compact / auto-compact”

**Shell allow-list:** literal `./helper` at command start. **Never** `cd … && ./helper`. **Never** construct blessed helper invocations via variables/indirection to dodge patterns. `pwd` once at startup; keep cwd at worktree root.

### 3.4 Subagents

**Subagent / Task / invoke is blocked during planning.** Do not spawn implementers while planning.  
When spawning in allowed contexts, load the **full** prompt file for that role (paths above) — do not improvise a weaker prompt.

### 3.5 Native plan mode / exit_plan_mode

Harness details. Real approval = path-named magic phrase. Continued draft feedback ≠ abandon unless user says start over / new cycle / abandon. Who may use native plan mode: **`AGENTS.md`** (planner/coder avoid; bare `run-grok` + orchestrator optional).

Native `enter_plan_mode` / `exit_plan_mode` / TUI **`a` (approve and start building)** is **not** VE execute. Harness `plan.md` and `.grok/plan.md` are process logs only — never the approved work plan. Planner/coder must not enter native plan mode (it cannot write `dev-ai-interaction/plans/`).

### 3.5a Grok 4.6 / Build 1.0 defaults do not override VE

Product defaults (4.6 “just do reversible work,” native plan **`a`**, background subagents, workflows/`/goal` on) **never** weaken §2–§3.

- Built-in “do clear reversible local work without asking” **never** authorizes tracked non-sandbox edits. Named sandbox plan + magic path approval still required.
- `ask_user_question` stays **on**. Answers inform the sandbox plan only — they are **not** magic approval and **not** permission to implement.
- Planner launchers force `GROK_SUBAGENTS=0` and `GROK_WORKFLOWS=0` (explicit `=1` may override for debug). Coder launchers force `GROK_WORKFLOWS=0` only. Orch / bare / master are unset unless the human sets env.
- User/config `default_reasoning_effort = "xhigh"` does **not** apply to `run-grok-coder` / `run-grok-master`: those launchers pin `--effort high` (`GROK_REASONING_EFFORT` overrides). Planner / orch / bare do not pass `--effort`. Execute **body** is a fresh spawn (`execution-subagent.md`, `isolation=none`) or a fresh coder process — not the long-lived parent transcript.
- `grok -c` / `--resume` keeps the transcript and the session’s **stored** model. It does **not** upgrade 4.5 → 4.6. Switch with `/model grok-4.6` after resume. Role launchers without `-c` start a **new** session.

### 3.6 Skills

`AGENTS.md` + `.grok/config.toml` `[skills].disabled` are authoritative. Do not treat bundled design/execute-plan/implement/pr-babysit/check-work as encouraged.

### 3.7 Long-lived handoff / new cycle (must not drop)

On execution handoff, also follow **`MULTI_AGENT_USER_INSTRUCTIONS.md`**: including writing `dev-ai-interaction/.post-handoff-gate.txt` when staying in the same long chat.  
“New planning cycle” master automation and planner restart: that doc + launchers. **Role pack requires reading MULTI_AGENT** for master/orchestrator and after handoff (see §10).

---

## 4. Execution quality

- **Before any edit:** re-read target file content; do not trust prior-turn memory (`State verification`).
- Write-tool success ≠ integrity. After edits: forensic read/grep of changed lines; `./build_app` per phase (STANDARD BLOCK).
- **Plan completeness (mandatory):** Before END/ready-to-test, re-read approved plan. Missing/reverted work in the **closed Critical Files** set → implement it. Unlisted paths: report in chat; **do not** implement. Blocked → stop, report, **no** ready-to-test.
- Plan Status: execute start → **APPROVED**; success → **CODE LANDED**; block → **BLOCKED — needs replan**. Stale DRAFT after ordered execute is not a human-facing “finding” — hygiene-fix or ignore when eng-log/git show execution.
- Product-intent PASS is not coder’s claim. Plan-scope complete or blocked only. Human/planner intent chat; **master Compliance Checker optional** (not required every execute).
- **Total turn reversion:** only during **active** execution before handoff — approved reset (§6), then replan. After handoff build, no auto-revert; feedback = new cycle. Revert handed-off work only with explicit user approval.

**Exact END marker** (must match STANDARD BLOCK):  
`**END OF EXECUTION TURN. Awaiting new directive or plan approval before any further source changes or investigation that leads to edits.**`  
then `results ready to test (new tag: ...)`.

---

## 5. Baseball and recovery

Strike = failed build / recoverable phase failure. 3 strikes = out → reset to last good phase/build tag. 3 outs = end of inning →  
`dev-ai-interaction/implementation-failure-logs/<date>-<slug>-inning-end.md`  
(template: `dev-ai-interaction/research/inning-end-report-template.md`) **before** replan. Recovery plans include **Already completed (exclude)**. Default ~3–8 coherent phases; finer only post–inning-end. Egregious failure = immediate out.

---

## 6. Git reset (three contexts only)

**Preflight:** `TAG=$(./get-builds-tag.sh)` then verify/reset. No inlined branch/tag scripts; no variable games to dodge whitelist.

1. **Uncommitted junk:** `git checkout .` / `git restore .` / `git reset --hard HEAD`
2. **Build/baseball recovery:** `git reset --hard builds` (master) or `git reset --hard <current-branch>/builds` only — never another branch’s tag
3. **Always forbidden:** **any relative ref** (`HEAD^`, `HEAD~`, `HEAD~N`, `HEAD@{n}`, …); arbitrary hashes unless user supplies one; `origin/*`; other agents’ tags

---

## 7. Geography and special files

- **Never** `..` in paths. Sandbox absolute path in §2. Orchestration root feeds `update-rules.sh`.
- **Rebase onto master:** follow `.grok/skills/rebase-on-master/SKILL.md`. Keep the feature copies of `ENGINEERING_LOG.md` / `TODO.md` / `project-facts.md`; do not merge specials. Other conflicts: stop and ask the human.
- **Worktree copy rule:** tracked copies must be committed on that branch or use `./update-rules.sh`. Uncommitted tracked dirt blocks `./build_app`. Gitignored binaries OK uncommitted.
- **Plans:** only user-**designated** files under `dev-ai-interaction/plans/`. Completed → `historical-plans/`. Never treat harness `~/.grok/sessions/**/plan.md` as approved work plan. Never read `historical-plans/` or non-designated plans for execution influence.
- **If you read a wrong/historical plan or continued after handoff:** (1) enter plan mode if available / treat self as planning-only, (2) revert unauthorized changes via §6, (3) report violation, (4) wait for user direction.
- **Filenames:** `descriptive-kebab-YYYYMMDD-HHMM-plan.md` (minutes when stamping; not day-only; seconds not required). New contract → new file + new minutes.
- **Plan content:** Context, Approach, Critical Files, reuse, **Phased Execution** (what/files/success), Verification/Acceptance. Cite STANDARD BLOCK by path only. No Mandate Acknowledgment. No ultra-micro except post–inning recovery. High-signal; soft ~2–8 KB typical. Style guide: `dev-ai-interaction/research/plan-style-guide.md`. **Critical Files is a closed set** (path + function + predicate; last line: files not listed → no edits). Finish greps in planning — no `grep` / `unless` / optional extra sites in the approved file. Fewest phases (one file often one phase). Completeness (§4) applies only to that set. Each plan stands alone (executor does not need the prior cycle’s chat).
- **project-facts:** full read launch/new cycle/before edit; orientation only; prune; no plan/branch/status narrative.
- **TODO:** future only; wrappers only.
- **ENGINEERING_LOG:** `./append-to-engineering-log` only.

---

## 8. Device logs

Single `adb logcat -d` (or device-specific) dump into sandbox; analyze locally. No serial device logcat thrash.

---

## 9. Engineering defaults

- `jq` for JSON. OCR multi-engine, no silent fallbacks. 4-DOF affine. Automated Word Veto primary.
- **Coordinates:** ICRS or raw pixels only — `docs/specs/ISOTROPIC_COORDINATE_SPEC.md`.
- **UI display:** `docs/reference/UI_COMPATIBILITY.md` (cite; do not paste). Code wins if conflict — update doc same commit.

### 9.1 Config and path defaults — fail loud (no dev-machine traps)

**Automatic fallbacks that are correct only on the development machine are a trap.** They paper over missing config so tests pass here, then fail later on another host, user, or checkout.

| Prefer | Avoid |
|--------|--------|
| **Require** explicit config (`project.config`, env, stamped `@@` + smudge) and **fail loudly** when unset/unsmudged | Silent defaults to this machine’s absolute paths, usernames, or layout |
| Tokens / empty → refuse host access or abort with a clear error | “If missing, use `/home/…` or `$HOME/git/…` so it still works for me” |
| Defaults only when **discussed and agreed** as the right thing for **anyone** using the tool, **wherever** they run it | Escape hatches like “allow non-\<primary user\>” that encode one human as the normal case |

**When a default is justified:** document why it is portable (not “works on the SoT laptop”). Machine-local values live in **gitignored** `project.config` (or equivalent), never as the success path in committed scripts.

Related: `docs/reference/PROJECT_CONFIG_LOCAL_ONLY.md` (local wiring; smudged tokens in tracked files).

---

## 10. Re-read policy

**Event-triggered pack reads win.** “Do not re-read every turn” only skips *routine* mid-phase chatter. It does **not** apply after a listed event — including while you are mid-plan, mid-execute, or “still in the same process.”

**Must re-read the role pack (table below) immediately after any of:**

- session start / fresh launch
- **`/compact` or harness auto-compact** (same event; ~85% context). Do this on the **first turn after** the compact, before other work
- new planning cycle
- execution start for a newly approved plan

A harness “plan mode still on” or compacted summary is **not** the pack. Memory of the pack after compact is **untrusted**.

| Everyone | + Planner | + Coder | + Master / orch after handoff |
|----------|-----------|---------|--------------------------------|
| `AGENT_CONTEXT.md`, overlay, this file, full `project-facts.md` | STANDARD BLOCK path, plan-style-guide, designated plan if any, full spawn prompt file when spawning | STANDARD BLOCK, **approved plan**, execution prompt file when spawning | `MASTER_AGENT_MANDATE.md` when master; **`MULTI_AGENT_USER_INSTRUCTIONS.md`** |

**Do not** re-read the full pack every ordinary turn or every phase gate. **Do not** use that skip to dodge an event above.

**Forbidden after compact:** “still the same process,” “don’t re-read every turn,” “I remember the mandates,” “compact reminder is enough.”

Critical rules are **not** optional on-demand guesses.

---

## 11. Roles (see also AGENTS.md)

| Role | Plans | Implements app |
|------|-------|----------------|
| Planner | Yes | No |
| Coder | No | Yes (approved plan only) |
| Master | No | Coordinate / merge / dispatch execute |
| Orchestrator / bare `run-grok` | Meta as needed | Meta; optional native plan mode |

Planner owns new cycles; failure logs under `implementation-failure-logs/`. Intent gaps after handoff → default **cleanup plan** when user wants fixes.

---

## 12. Human / launcher detail

Magic phrases, never-say lists, two-terminal ritual: **`MULTI_AGENT_USER_INSTRUCTIONS.md`**. Launcher inject strings load prompt files under `.grok/prompts/`. Do not re-author conflicting ultra-micro / paste-STANDARD-BLOCK text in launchers.

---

