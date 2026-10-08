## STANDARD BLOCK — cite this path; do not paste it

- Implement the plan’s Phases and Critical Files. A path that is missing or outside this checkout is a question, not an edit. See `AGENT_MANDATES.md` “Plans and paths”.
- First shell: `./exec-preflight` with the plan path. Then `./append-to-engineering-log @file`. Set Status **APPROVED**.
- Each phase: those edits, a forensic read of them, then a successful `./build_app` before the next phase. `./build_app` commits. Do not `git add`.
- Retry and reset: `AGENT_MANDATES.md` “Retry”. The reset command is `./reset-to-builds.sh`.
- Before handoff, re-read the plan. Missing work in Critical Files gets finished. If it cannot, Status **BLOCKED — needs replan**, report, and do not say ready to test.
- On success, Status **CODE LANDED**, then exactly:
  `**END OF EXECUTION TURN. Awaiting new directive or plan approval before any further source changes or investigation that leads to edits.**`
  then `results ready to test (new tag: ...)`.
- `project-facts.md` is orientation. `TODO.md` is future work through its helpers. The log is `./append-to-engineering-log` only.
