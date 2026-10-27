# project-facts.md (trigger index)

This is shared knowledge for every agent working on the project: a map of what is where, and what exists to be found (spec and policy documents, helpers, conventions), so agents do not search for it. Never put the status of a branch, plan, or roadmap here. Keep it as small as that allows; leaving something out costs far more than the bytes (a large search, or an implementation that misses a spec). Detail lives in `docs/facts/*.md`, behind a trigger line here.

## Triggers
- Before touching <area/path>: read `docs/facts/<file>.md`.
- Before building or testing: read the Testing section below.
- Before editing TODO.md, ENGINEERING_LOG.md, or this file: use `./todo-append`, `./todo-close`, `./append-to-engineering-log`. To read them: `./todo-open`, `./englog-recent`; never read ENGINEERING_LOG.md whole.

## Build and commit
- **Build type:** android-gradle | command | none
- **Standard build:** debug
- **Test build:** qa | instrumentation:<variant> | none
- Build: `./build_app --standard` (add `--test` unless Test build is none; never `--play`).

## Testing
Project-wide capabilities: unit=<yes/no cmd> emulator=<yes/no> device=<yes/no> vehicle-only=<list>.
Changing this section needs David's OK (master-merge asks).

## Worktree: <name>
Testing override: device=<id or none> emulator=<yes/no>.

Coder children follow `roles/coder.md`. A plan line `Review: diff` adds one reviewer. Do not put a child sequence in this file.
