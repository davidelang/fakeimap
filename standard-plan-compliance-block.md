## STANDARD BLOCK: cite this path, do not paste it
- Implement the plan's Phases and Critical Files and nothing else.
- First shell (not for a pin bump): `./exec-preflight <plan>`, then `./append-to-engineering-log @file`.
- Each phase: make the edits, do a forensic read of them, then get a green `./build_app` before the next phase. `./build_app` commits.
- Pin bump (the plan changes only `third_party/<lib>/{libpin.toml,SOURCE.md}`): one phase. `third_party/fetch-deps refresh <lib>`, then `build <lib>`; `status <lib>` shows the new sha; then one `./build_app`. No lib tests, no preflight, no engineering-log entry.
- Before handoff, re-read the plan. Finish any missing Critical Files work. If you cannot, set Status **BLOCKED** and end with `STATUS: BLOCKED needs replan`.
- On success, set Status **CODE LANDED** and end with `STATUS: SUCCESS ready to test (tag <tag>)`.
