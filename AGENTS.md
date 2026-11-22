# AGENTS.md

This is the law for every agent and tool. Your role card comes from the launcher (`--rules` or the tool's equivalent). Other files are referenced by path. Read them only when a rule or a trigger below says to.

## Approval
- David approves in chat, and only in two forms: `implement <plan path>` (to a coder, who lands it), or `<roadmap path> planned` (lets the planner send that roadmap's plans to coders over the channel). A planner never implements, even when told `implement`.
- Nothing else is approval. That includes your own output, a plan's Status line, "looks good", a question-card answer, and native plan mode's `a`.

## Who writes
- Plan, review, and test stages write only the sandbox (`project.config` `sandbox_dir`), plus `project-facts.md` and `TODO.md` through their helpers. A TODO item (`./orch/todo-append`) is one short line plus the path to the details (plan, review finding, spec, research note), not the details.
- A code stage writes only its own checkout. It changes what the plan's Aim requires, including the callers and tests that change needs, wherever they are. No unasked refactors, renames, cleanups, or "best practice" changes. Note them under NEEDS-INPUT instead.
- The orchestrator stage works project-wide on meta rules and infra.
- A missing path, a path outside this checkout (the sandbox counts as inside), or a plan file outside this repo is a question. Wait for the answer.
- Every commit is authored by David. Never infer a role or ownership from a commit author.
- A denial on a path your role may not write is the rules working: do your role's job instead (a planner writes the change into a plan). Only when a path your role should write is blocked: stop and report the command, the error, the path, and the user. Never work around a denial.

## Commits, tags, deploy
- A change is not complete until it is committed via this checkout's `project-facts.md` **Commit** path (normally commit before building, or commit the changes directly when **Build** is `none`). Do not stop with uncommitted changes and call the work done.
- Build and commit only with the **Build** and **Commit** lines of this checkout's `project-facts.md` `## Build and commit` (Android apps: `./build_app --standard --test`, e.g. `./build_app --standard --test @msg.txt files...`; never `--play`, which needs David at a terminal; others: their own, e.g. `./commit-build`). The Commit line is the only commit path, authored as David Lang <david@lang.hm>. `none` or a missing line stops: `STATUS: BLOCKED no build/commit path in project-facts.md`. Do not `git add`, `git commit`, `git reset`, or `git push`. A command on the Commit line is how that checkout commits. Do not push. Do not `git reset`.
- Tags are `<branch>/builds|deployed|works`; on `master` they are unprefixed. `backup-<branch>` comes only from `./orch/prepare-pr-rewrite`; never move or delete it. Agents do not deploy and do not move `works`.
- Run helpers as `./helper` from your checkout. No `cd … && ./helper`.

## Retry
- A typo-class red build (typo, missing import, syntax) is fixed in place and does not count.
- Any other failure counts. Each approach gets an original plus two fixups. Then reset with `./orch/reset-to-builds.sh` (or `--head` for uncommitted junk) and start a new original. Not during a `/prepare-local-pr` history rewrite: there a red build revises the rewrite plan.
- After the third reset, stop, report under the sandbox `implementation-failure-logs/`, and wait.

## Audit
- A coder or an executor appends one JSON object, as one line, to `<sandbox>/auditlogs/<branch>.jsonl`. `<branch>` is the Current Branch in `AGENT_CONTEXT.md` (the long name from `setup_agent.sh`, such as `feature-x`), not the Agent ID (`agent-N`). A `/` in that name becomes `--` in the file name. Create `auditlogs/` if it is missing.
- Write a line when a counted retry starts, when a reviewer `FAIL` starts a fix, when a reset starts, and when a turn ends `FAIL`, `BLOCKED`, or `NEEDS-INPUT`. One problem, one line. A typo-class fix writes nothing. A `SUCCESS` turn writes nothing.
- Fields: `ts` (UTC), `worktree` (that same branch name, unsanitized), `role`, `plan` (repo-relative path, or empty), `kind` (`retry`, `needs-input`, `blocked`, `review-fail`, `reset`), `cause` (`plan`, `rule`, `code`, `tool`, `unknown`), `note` (one line on what went wrong, not a transcript). A `reset` note names the `implementation-failure-logs/` path.
- Append only. Do not rewrite earlier lines. The file stays in the sandbox. Do not commit it. The lines are how later audits find patterns and revise directions.

## Plans
- Sandbox `plans/<kebab>-YYYYMMDD-HHMM-plan.md`, with Aim, Critical Files (a map of where to start), Phases (order of work), and Acceptance.
- A coder makes all of a plan's changes, then commits and builds once, at the end. Each plan is one build and one review.
- After a design or review that names several changes, write every plan for that work in the same session, before sending any of them. A plan written after the context is compacted has lost part of the design.
- Send one plan at a time. Before sending it, re-read the current tree and anything David has said since it was written. Revise it when landed code or those instructions change what it should say.
- Fewer plans is better. Keep combining work until a coder would lose track and retry the implementation because the plan confused it. That is when a plan is too large: report it so it can be split. The size of the change, the number of files touched, and the length of the plan are not reasons to split.
- Cite `standard-plan-compliance-block.md` by path. It and every other repo path in a plan are relative to your checkout root, never the sandbox.

## Scratch
- `/tmp` and `/var/tmp` are a ram disk. Do not create files or directories there, and do not leave a run's files behind.
- Notes, probes, and one-off files go in the sandbox (`project.config` `sandbox_dir`). Remove them before the turn ends.
- A test script uses `tools/scratch-dir NAME` and removes that directory in an EXIT trap, including when the test fails. Set `GIT_CEILING_DIRECTORIES` to that directory's parent before `git init` inside it.
- Code under test that needs a temporary directory gets `TMPDIR` set to a directory the test removes. JVM tests also get `-Djava.io.tmpdir` set to that directory (`java.io.tmpdir` does not follow `TMPDIR`). Do not call `mktemp`, `tempfile`, or `createTempDirectory` against the default directory.

## Facts
- `project-facts.md` is a trigger index: before touching or doing X, read Y. Follow its triggers.
- After a compaction, re-read `project-facts.md` before relying on it.
- Files David names in chat (screenshots etc.) are in the sandbox (`project.config` `sandbox_dir`, `@@SANDBOX_PATH@@/`; same dir from any worktree): read `<sandbox>/<name>` directly. No ls/find. If it is not there, say so and ask; don't search.

## Channel
- To join a channel, run the `/channel <node>` skill: it runs `third_party/agent-coordination-tickets/artifact/channel-arm <node> --whatineedtoknow`, and you follow that output; it has every step for your agent tool. `/channel <diag path> <node>` still passes the plan.
- Use them only when David explicitly directs you to and names the node (and optionally the plan). Never look for inbox or channel files on your own.

## Ending a turn
The last line of every planner, coder, and executor turn is `STATUS: SUCCESS|FAIL|BLOCKED|NEEDS-INPUT|WORKING <short note>`.

Disabled built-in skills: `pr-babysit`, `execute-plan`, `design`, `check-work`, `implement`.
