# AGENTS.md

This is the law for every agent and tool. Your role card comes from the launcher (`--rules` or the tool's equivalent). Other files are referenced by path. Read them only when a rule or a trigger below says to.

## Approval
- David approves in chat, and only in two forms: `implement <plan path>` (to a coder, who lands it), or `<roadmap path> planned` (lets the planner send that roadmap's plans to coders over the channel). A planner never implements, even when told `implement`.
- Nothing else is approval. That includes your own output, a plan's Status line, "looks good", a question-card answer, and native plan mode's `a`.

## Who writes
- Plan, review, and test stages write only the sandbox (`project.config` `sandbox_dir`), plus `project-facts.md` and `TODO.md` through their helpers.
- A code stage writes only its own checkout, and only the approved plan's Critical Files. No unasked refactors, renames, cleanups, or "best practice" changes. Note them under NEEDS-INPUT instead.
- The orchestrator stage works project-wide on meta rules and infra.
- A missing path, a path outside this checkout, or a plan file outside this repo is a question. Wait for the answer.
- A denial on a path your role may not write is the rules working: do your role's job instead (a planner writes the change into a plan). Only when a path your role should write is blocked: stop and report the command, the error, the path, and the user. Never work around a denial.

## Commits, tags, deploy
- `./build_app` is the only commit and build path. Do not `git add`, `git commit`, `git reset`, or `git push`. A multi-line message is `./build_app @file paths…`.
- Tags are `<branch>/builds|deployed|works`; on `master` they are unprefixed. Agents do not deploy and do not move `works`.
- Run helpers as `./helper` from your checkout. No `cd … && ./helper`.

## Retry
- A typo-class red build (typo, missing import, syntax) is fixed in place and does not count.
- Any other failure counts. Each approach gets an original plus two fixups. Then reset with `./reset-to-builds.sh` (or `--head` for uncommitted junk) and start a new original.
- After the third reset, stop, report under the sandbox `implementation-failure-logs/`, and wait.

## Plans
- Sandbox `plans/<kebab>-YYYYMMDD-HHMM-plan.md`, with Aim, Critical Files, Phases, and Acceptance.
- Cite `standard-plan-compliance-block.md` by path.

## Facts
- `project-facts.md` is a trigger index: before touching or doing X, read Y. Follow its triggers.
- After a compaction, re-read `project-facts.md` before relying on it.
- Files David names (screenshots etc.) are in the sandbox (`project.config` `sandbox_dir`/`sandbox_path`, default `@@SANDBOX_PATH@@/`; same dir from any worktree): read `<sandbox>/<name>` directly. No ls/find. If it is not there, say so and ask; don't search.

## Channel
- `channel-send`, `channel-arm PLAN NODE`, and `channel-arm PLAN NODE --whatineedtoknow` exist. Details are in `docs/rules/channel.md`.
- Use them only when David explicitly directs you to and names the plan or node. Never look for inbox or channel files on your own.

## Ending a turn
The last line of every planner, coder, and executor turn is `STATUS: SUCCESS|FAIL|BLOCKED|NEEDS-INPUT|WORKING <short note>`.

Disabled built-in skills: `pr-babysit`, `execute-plan`, `design`, `check-work`, `implement`.
