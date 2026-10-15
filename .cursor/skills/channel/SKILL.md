---
name: channel
description: >
  Join an agent channel as a node: set up the inbox if missing, learn the rules
  from --whatineedtoknow, and start the watch. Use only when David runs
  /channel <diag path> <node> or names the plan and node.
---

# channel

Arguments: `<diag>` (plan file path) and `<node>`. Run from the repo root.

1. Binary dir `B`: `third_party/agent-coordination-tickets/src/go/` if `channel-arm` is there, else `$GIT_HOME/agent-coordination-tickets/go/` (see `docs/rules/channel.md`).
2. If `$B/channel-arm <diag> <node> --whatineedtoknow` shows an inbox that does not exist, run `$B/channel-setup <diag> <node>` (safe to rerun).
3. Run `$B/channel-arm <diag> <node> --whatineedtoknow` and follow its "how to act" header. It has the paths, the send forms, and the body rules.
4a. First review `<inbox>/claimed/`: work you already started. Check whether your reply was already sent before redoing it. Never reclaim by age; report stale claims.
4b. Start the watch as a background shell (`block_until_ms: 0`): `.cursor/lib/cursor-channel-watch <diag> <node>`. Each `FILE <path>` line is one message: handle the oldest, then end the turn. If that shell ends, start it again. The watch answers PING itself and passes on a check-all PING.
5. Send with `channel-send ... DEST VERB` and the body as BODYFILE, `-m 'TEXT'` (repeatable), or stdin; add REF files (several allowed).
6. `rm` the message file when done (from `claimed/` only after your reply is sent).
7. After a compaction, rerun steps 3 and 4a.

Checks: `$B/channel-ping <diag> <node>` tests every arrow now (never rate-limited); `--all` checks every link reachable from the node (each node passes it on once per run, at most every 30 min). Without a watch, answer a PING with PONG and, if its body has `SCOPE: all` and `RUN: <id>`, run `$B/channel-ping --run <id> <diag> <node>`; `$B/channel-status <diag>` shows inbox counts, ages, and claims.
