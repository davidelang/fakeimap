---
name: prepare-local-pr
description: >
  Prepare a local multi-agent PR for this project (not GitHub). Pre-submit code
  review vs approved plan, history rewrite into a logical progression (original
  tip kept as tag backup-<branch>), generate_pr.sh to
  $sandbox_dir/PRs/. Use when asked to prepare PR, generate PR, cleanup
  history for merge, or /prepare-local-pr. Never use gh pr create, Graphite, or pr-babysit.
  Same skill on VehicleExpenses and first-party library hosts.
---

# prepare-local-pr (Cursor, Coder)

Read `docs/rules/prepare-local-pr.md` and follow it exactly; it is the whole procedure. Helpers: `./prepare-pr-rewrite`, the project-facts.md Build and Commit path, `./generate_pr.sh`, `./append-to-engineering-log`.

- Master launcher: `./run-cursor-master`.
- Permissions (`.cursor/cli.json` `Shell(...)` allow list): the helpers are allowed. Never run raw `git add`, `git commit`, `git reset`, `git tag`, `git rebase` or `git push`; use the helpers.
