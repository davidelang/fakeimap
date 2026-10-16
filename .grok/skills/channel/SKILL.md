---
name: channel
description: >
  Join an agent channel as a node: run the connector's --whatineedtoknow and
  follow it. Use only when David runs /channel <diag path> <node>.
when-to-use: "/channel <diag path> <node>"
---

# channel

Arguments: `<diag>` (plan file path) and `<node>`. From the repo root run exactly:

`third_party/agent-coordination-tickets/artifact/channel-arm <diag> <node> --whatineedtoknow`

Then do what its output says, in order, now; it is written for this agent tool and this node. Do not explore: do not search for binaries, inboxes, or other docs.

If the command does not run, prints an error, or its first line is not `how to act:`, reply only `STATUS: BLOCKED <the error, or: channel artifact missing/too old in third_party/agent-coordination-tickets/artifact>` and stop. Do not fetch, build, or work around it.
