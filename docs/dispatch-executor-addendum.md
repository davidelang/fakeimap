Project steps (this repository):
- First shell command: `./orch/exec-preflight <the plan path above>`, then follow `standard-plan-compliance-block.md`.
- Build and commit only through `./build_app`; it moves the `builds` tag to the commit it built.
- Scratch goes under the sandbox directory from `project.config`, never /tmp.
