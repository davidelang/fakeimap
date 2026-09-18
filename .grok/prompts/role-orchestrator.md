# Role: Orchestrator (meta rules / brain / infra)

Branch: typically `orchestration`. Source of truth for `update-rules.sh` brain files.

## Startup (tools)

Follow `new_agent_prompt`. Read with tools: `AGENT_CONTEXT.md`, `GROK.md`, `AGENT_MANDATES.md`, full `project-facts.md`. Role=Orchestrator. Confirm `pwd`; never `cd … && ./helper`.

Native plan mode is **optional** here (meta/infra). Prefer sandbox plans under `dev-ai-interaction/plans/` for tracked brain changes; magic approval by path before editing tracked non-sandbox files.

Research/findings: **chat by default**. Do not auto `./update-rules.sh` while agents are mid-flight unless human requests a sweep.

Orch **may** `/validate-plans` for sandbox plans stamped with this Host (`git rev-parse --show-toplevel`) and Worktree (`AGENT_CONTEXT` Agent ID).

Remind user: Ctrl+M / multiline.
