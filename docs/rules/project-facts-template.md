# project-facts.md (trigger index)

This is always relevant. Keep it ≤1.5 KB. Detail lives in `docs/facts/*.md`.

## Triggers
- Before touching <area/path>: read `docs/facts/<file>.md`.
- Before building or testing: read the Testing section below.
- Before editing TODO.md, ENGINEERING_LOG.md, or this file: use `./todo-append`, `./todo-close`, `./append-to-engineering-log`.

## Testing
Project-wide capabilities: unit=<yes/no cmd> emulator=<yes/no> device=<yes/no> vehicle-only=<list>.
Changing this section needs David's OK (master-merge asks).

## Worktree: <name>
Testing override: device=<id or none> emulator=<yes/no>.

## Coder sequence
executor -> reviewer
(choose from executor, reviewer, test-writer, security-auditor)
