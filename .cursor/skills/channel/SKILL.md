---
name: channel
description: >
  Join an agent channel as a node: check the pin, set up the inbox, learn the
  rules from --whatineedtoknow, and start the watch. Use only when David runs
  /channel <diag path> <node> or names the plan and node.
---

# channel

Arguments: `<diag>` (plan file path) and `<node>`. Run every command from the repo root, exactly as written, in this order. `$B` is `third_party/agent-coordination-tickets/src/go`; there is no other location.

Do not explore: do not glob or search for binaries or inboxes, and do not read `docs/rules/channel.md`, the watch script, or terminal logs. The header from step 1 is all you need.

1. `third_party/agent-coordination-tickets/src/go/channel-arm <diag> <node> --whatineedtoknow`
   If it does not run, or its first line is not `how to act:`, STOP. Reply only `STATUS: BLOCKED channel pin missing/too old; run fetch-deps` and do nothing else.
2. `third_party/agent-coordination-tickets/src/go/channel-setup <diag> <node>` (safe to rerun; prints `existing` when already set up).
3. Follow the header from step 1. First review `<inbox>/claimed/` as it says.
4. Start the watch: run exactly `.cursor/lib/cursor-channel-watch <diag> <node>` as a background shell (`block_until_ms: 0`). Never run `channel-arm` yourself for the watch, even though the header's `watch:` line names it; this wrapper runs it for you.
   If it prints `ALREADY-WATCHING`, a watch for this node is already running: do not start another. If it prints `WATCH-ENDED`, report the message before it and stop. If the shell ends with neither, start it again (it shows the last `FILE` again).
5. Each `FILE <path>` is one message: handle the oldest, send with `$B/channel-send` as the header shows, `rm` the file when done, then end the turn.
6. After a compaction, rerun step 1 and review `claimed/`. Do not restart the watch shell if it is still running.

Checks, only when asked: `$B/channel-ping <diag> <node>` tests your arrows (`--all`: every reachable link); `$B/channel-status <diag>` shows inbox counts, ages, and claims.
