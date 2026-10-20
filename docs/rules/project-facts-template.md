# project-facts.md (trigger index)

This is always relevant. Keep it ≤1.5 KB. Detail lives in `docs/facts/*.md`.

## Triggers
- Before touching <area/path>: read `docs/facts/<file>.md`.
- Before building or testing: read the Testing section below.
- Before editing TODO.md, ENGINEERING_LOG.md, or this file: use `./todo-append`, `./todo-close`, `./append-to-engineering-log`.

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
