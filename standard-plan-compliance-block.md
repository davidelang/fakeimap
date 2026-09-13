## COMPLIANCE & EXECUTION GUARDRAILS (STANDARD BLOCK v2026-07-31 — REFERENCE ONLY; DO NOT PASTE INTO PLANS)

Plans must **cite this file by path** (one line). Do **not** paste this block into plan documents.

- **Scope:** Implement only observable changes in the plan's **Phased Execution** / product locks. No scope creep.

- **Per-phase gates** (details: `AGENT_MANDATES.md`):
  - **Start:** Re-read approved plan + `project-facts.md` (full); first action: write a sandbox entry if needed, then `./append-to-engineering-log @file` (never ritual TODO; not a quoted multi-line argv). Set plan **Status: APPROVED**. **No `cd … &&` on helpers** — cwd fixed after startup.
  - **Each phase:** Phase-only edits → forensic read/grep → `git add` (sources + `ENGINEERING_LOG.md` if appended) → successful `./build_app` before the next phase.
  - **Completeness (before handoff):** Re-read plan contract. Missing/reverted in-scope work → implement it. If blocked → **Status: BLOCKED — needs replan**, report gaps, do **not** ready-to-test.
  - **Granularity:** Coherent independently verifiable phases (~3–8 typical). Finer only after end of inning (3 outs) via End of Inning Report.
  - **Recovery:** Reset only via `./get-builds-tag.sh` preflight. **3rd out:** inning-end report before replan.

- **Hygiene:** `project-facts.md` = orientation only; `TODO.md` future-only via `todo-append`/`todo-close`; `ENGINEERING_LOG` via `./append-to-engineering-log` only; sandbox `/home/dlang/git/VehicleExpenses-automated/dev-ai-interaction/`.

- **Handoff:** After completeness + final build + verification, set plan **Status: CODE LANDED**, emit exactly:  
  `**END OF EXECUTION TURN. Awaiting new directive or plan approval before any further source changes or investigation that leads to edits.**`  
  then `results ready to test (new tag: ...)`.  
  Product-intent chat is human/planner; master Compliance Checker is **optional**. Do not claim product-intent PASS as coder.

- **Standing rules:** `AGENT_MANDATES.md`, `MULTI_AGENT_USER_INSTRUCTIONS.md`, role pack in `AGENTS.md` (re-read on launch, **`/compact` or auto-compact**, new cycle, execute start). “Not every turn” never cancels those events.
