---
name: channel
description: >
  Join an agent channel as a node: run the connector's --whatineedtoknow and
  follow it. Use when David runs /channel <node>, /channel <diag path> <node>,
  or /channel rebind.
---

# channel

From the repo root. Do not explore: do not search for binaries, inboxes, or other docs.

## /channel <node>

Run exactly:

`third_party/agent-coordination-tickets/artifact/channel-arm <node> --whatineedtoknow`

If David also named the plan (`/channel <diag> <node>`), run:

`third_party/agent-coordination-tickets/artifact/channel-arm <diag> <node> --whatineedtoknow`

Then do what its output says, in order, now; it is written for this agent tool and this node.

If the command does not run, prints an error, or its first line is not `how to act:`, reply only `STATUS: BLOCKED <the error, or: channel artifact missing/too old in third_party/agent-coordination-tickets/artifact>` and stop. Do not fetch, build, or work around it.

## /channel rebind

David started a new conversation (`/new`) and the old watch is still running without this session. Run exactly:

`third_party/agent-coordination-tickets/artifact/channel-rebind`

Then:

- First line `how to act:` — follow it, in order, now (same as `/channel <node>`).
- First line `REBIND-CHOOSE` — show David each `pid=... plan=... node=...` line and wait; then run `third_party/agent-coordination-tickets/artifact/channel-rebind --pid <the pid he picked>` and follow that output the same way.
- First line `REBIND-NONE` — tell David nothing in this worktree was left to rebind, and stop.
- Anything else (error, missing binary) — `STATUS: BLOCKED <the error, or: channel artifact missing/too old in third_party/agent-coordination-tickets/artifact>` and stop.

Do not pass a diag or node. Do not start a watch until this command has printed `how to act:`.
