## STANDARD BLOCK: cite this path, do not paste it
- Implement the plan's Phases and Critical Files and nothing else.
- First shell (not for a pin bump): `./exec-preflight <plan>`, then `./append-to-engineering-log @file`.
- Each phase: make the edits, `git diff` those paths against that phase's observable, then commit and build green with the project-facts.md Commit and Build path before the next phase.
- Pin bump (the plan changes only `third_party/<lib>/{libpin.toml,SOURCE.md}`): one phase. `third_party/fetch-deps refresh <lib>`, then `build <lib>`; `status <lib>` shows the new sha; then one commit and build (project-facts.md path). No lib tests, no preflight, no engineering-log entry.
- Before handoff, re-read the plan once. Finish any missing Critical Files work. If you cannot, set Status **BLOCKED** and end with `STATUS: BLOCKED needs replan`.
- On success, set Status **CODE LANDED** and end with `STATUS: SUCCESS ready to test (tag <tag>)`.
