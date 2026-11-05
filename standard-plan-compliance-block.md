## STANDARD BLOCK: cite this path, do not paste it
- Implement the plan's Aim. Critical Files is where to start; change whatever else the Aim actually requires, including callers and tests. Nothing unrelated: no refactors, renames, or cleanups (list them under NEEDS-INPUT).
- First shell (not for a pin bump): `./orch/exec-preflight <plan>`, then `./orch/append-to-engineering-log @file`.
- Do the phases in order; they order the work and are not commit points. When all edits are made, `git diff` against Acceptance, then commit and build once with the project-facts.md Commit and Build path. If it cannot build green within the retry rules in `AGENTS.md`, end `STATUS: BLOCKED needs replan` and say what to split.
- Pin bump (the plan changes only `third_party/<lib>/{libpin.toml,SOURCE.md}`): one phase. `third_party/fetch-deps refresh <lib>`, then `build <lib>`; `status <lib>` shows the new sha; then one commit and build (project-facts.md path). No lib tests, no preflight, no engineering-log entry.
- Before handoff, re-read the plan once and check every Acceptance line. Finish anything missing. If you cannot, set Status **BLOCKED** and end with `STATUS: BLOCKED needs replan`.
- On success, set Status **CODE LANDED** and end with `STATUS: SUCCESS ready to test (tag <tag>)`.
