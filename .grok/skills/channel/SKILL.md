---
name: channel
description: >
  Join an agent channel as a node: set up the inbox if missing, learn the rules
  from --whatineedtoknow, and start the watch. Use only when David runs
  /channel <diag path> <node> or names the plan and node.
when-to-use: "/channel <diag path> <node>"
---

# channel

Arguments: `<diag>` (plan file path) and `<node>`. Run from the repo root.

1. Binary dir `B`: `third_party/agent-coordination-tickets/src/go/` if `channel-arm` is there, else `$GIT_HOME/agent-coordination-tickets/go/` (see `docs/rules/channel.md`).
2. If `$B/channel-arm <diag> <node> --whatineedtoknow` shows an inbox that does not exist, run `$B/channel-setup <diag> <node>` (safe to rerun).
3. Run `$B/channel-arm <diag> <node> --whatineedtoknow` and follow its "how to act" header. It has the paths, the send command, and the body rules.
4. Start the watch with the `monitor` tool, `persistent: true`: `$B/channel-arm <diag> <node>`. Each `FILE <path>` line is one message: handle the oldest, then end the turn.
5. Send with `channel-send ... DEST VERB BODYFILE`: write the body to a file first.
6. `rm` the message file when done (if you moved it to `inbox/claimed/`, rm it only after your reply is sent).
7. After a compaction, rerun step 3. Do not restart the monitor if it is still running.
