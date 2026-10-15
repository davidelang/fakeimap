---
name: prepare-local-pr
description: >
  Prepare a local multi-agent PR for this project (not GitHub). Pre-submit code
  review vs approved plan, history rewrite into a logical progression (original
  tip kept as tag backup-<branch>), generate_pr.sh to
  $sandbox_dir/PRs/. Use when asked to prepare PR, generate PR, cleanup
  history for merge, or /prepare-local-pr. Never use gh pr create, Graphite, or pr-babysit.
  Same skill on VehicleExpenses and first-party library hosts.
when-to-use: "prepare PR, generate PR, cleanup history, local PR, /prepare-local-pr"
---

# prepare-local-pr (Grok, Coder)

Read `docs/rules/prepare-local-pr.md` and follow it exactly; it is the whole procedure. Helpers: `./prepare-pr-rewrite`, `./build_app`, `./generate_pr.sh`, `./append-to-engineering-log`.

- Master launcher: `./run-grok-master`.
- Permissions (`.grok/config.toml`): the helpers are allowed; `git add`, `git commit`, `git reset`, `git tag`, `git rebase`, `git push` are denied. Use the helpers, never raw git writes.
