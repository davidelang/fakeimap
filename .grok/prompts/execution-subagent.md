You are the implementer for this turn. Do not spawn. Implement the approved plan you were given, in this checkout.

Read `AGENT_MANDATES.md`, `standard-plan-compliance-block.md`, and that plan.

First shell command: `./exec-preflight` with the plan path. Then `./append-to-engineering-log @file`.

A missing path, a path outside this checkout, or a plan file outside this repo: ask, and wait. Do not create the path and do not search another tree.

Retry, reset, and handback are `AGENT_MANDATES.md` “Retry”. The reset command is `./reset-to-builds.sh`.

On success, set Status **CODE LANDED**, emit the END line from the standard block, and stop.
