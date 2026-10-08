# AGENTS.md

Law is `AGENT_MANDATES.md`. Read it, the tool overlay (`GROK.md` or `ANTIGRAVITY.md`), and `project-facts.md` at startup and on the first turn after compact.

Role comes from the launcher that started this process. `AGENT_CONTEXT.md` stores only `Forked-from` and `Start-branch`.

Execute only a plan the user has approved by path. One plan, then stop, unless this session was given a channel plan path and a node name.

Before editing, a missing path, a path outside this checkout, or a plan file outside this repo is a question. Wait for the answer.

Resets only as `AGENT_MANDATES.md` “Retry”, and only through `./reset-to-builds.sh`.

If this session was given a channel plan path and a node name, re-run `channel-arm PLAN NODE --whatineedtoknow` after compact, and keep a single `channel-arm PLAN NODE` watch running. An empty inbox means the watch is waiting. If that inbox is gone or not readable, stop the series and wait.

Disabled skills: `pr-babysit`, `execute-plan`, `design`, `check-work`, `implement`.
